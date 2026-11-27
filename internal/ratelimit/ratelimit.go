// Package ratelimit provides in-memory, bounded rate limits for the authentication routes (N5): request budgets
// (token buckets) and failure counters with temporary lockouts. It knows nothing about HTTP or PocketBase; internal/auth
// decides what the keys are and which attempts failed.
//
// Both kinds keep one entry per key in a table of bounded size: when it is full, the least recently used entry is
// dropped (entries with an attempt in flight are kept). Entries that carry no information any more (a full bucket, a
// counter without failures) are removed as they are found, so a table holds little more than the keys that failed
// recently. Everything is safe for concurrent use.
package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

// Outcome is the result of an attempt admitted by [Lockouts.Acquire].
type Outcome int

const (
	// Neutral attempts neither count as failures nor reset the counter (a malformed request, a server error, or a
	// success that must not reset this counter).
	Neutral Outcome = iota
	// Failure counts towards the lockout.
	Failure
	// Success forgets the key's failures.
	Success
)

// BusyWait is the wait reported when a key's remaining attempts are all in flight: one of them may yet lock the key.
const BusyWait = time.Second

// Policy configures [Lockouts]. Threshold failures are allowed; the Threshold-th failure locks the key for Lockout,
// and every further failure locks it again for twice as long as the previous lockout, up to MaxLockout. After a
// lockout ends, one attempt is allowed at a time. Failures are forgotten after Forget without a new failure (it should
// be longer than MaxLockout, or waiting out the longest lockout would forget the failures), or when a success is
// reported. A Threshold of zero or less disables the policy.
type Policy struct {
	Threshold  int
	Lockout    time.Duration
	MaxLockout time.Duration
	Forget     time.Duration
}

// lockout returns the length of the lockout after failures failures (failures >= Threshold).
func (p Policy) lockout(failures int) time.Duration {
	d := p.Lockout
	for i := p.Threshold; i < failures && d < p.MaxLockout; i++ {
		d *= 2
	}
	return min(d, p.MaxLockout)
}

// Lock describes a lockout that a reported failure started.
type Lock struct {
	// Duration is how long the key is locked; zero when the failure did not lock it.
	Duration time.Duration
	// First is true for the first lockout after the key had fewer than Threshold failures.
	First bool
	// Failures is the number of failures counted.
	Failures int
}

type lockState struct {
	failures    int
	inflight    int
	lastFailure time.Time
	lockedUntil time.Time
}

// Lockouts counts failed attempts per key and locks keys that fail too often (see [Policy]).
//
// Call [Lockouts.Acquire] before an attempt and, if it was admitted, [Lockouts.Release] with its outcome after it.
// An attempt is admitted only while the key is not locked and fewer attempts are in flight than failures remain
// before the next lockout, so concurrent attempts cannot get past the threshold by racing each other.
type Lockouts struct {
	mu     sync.Mutex
	policy Policy
	now    func() time.Time
	table  table[*lockState]
}

// NewLockouts returns failure counters with policy, holding at most capacity keys. now is the clock; nil means
// time.Now.
func NewLockouts(policy Policy, capacity int, now func() time.Time) *Lockouts {
	if now == nil {
		now = time.Now
	}
	return &Lockouts{policy: policy, now: now, table: newTable[*lockState](capacity)}
}

// idle reports whether s carries no information at t.
func (l *Lockouts) idle(s *lockState, t time.Time) bool {
	return s.inflight == 0 && !t.Before(s.lockedUntil) &&
		(s.failures == 0 || t.Sub(s.lastFailure) >= l.policy.Forget)
}

// forget clears the failures of s once they are old enough.
func (l *Lockouts) forget(s *lockState, t time.Time) {
	if s.failures > 0 && !t.Before(s.lockedUntil) && t.Sub(s.lastFailure) >= l.policy.Forget {
		s.failures = 0
	}
}

// Acquire admits an attempt for key, or reports how long to wait before trying again.
func (l *Lockouts) Acquire(key string) (ok bool, wait time.Duration) {
	if l.policy.Threshold <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	l.table.sweep(func(s *lockState) bool { return l.idle(s, t) })
	s, found := l.table.get(key)
	if !found {
		s = &lockState{}
		l.table.put(key, s, func(s *lockState) bool { return s.inflight > 0 })
	}
	l.forget(s, t)
	if t.Before(s.lockedUntil) {
		return false, s.lockedUntil.Sub(t)
	}
	allowed := 1
	if s.failures < l.policy.Threshold {
		allowed = l.policy.Threshold - s.failures
	}
	if s.inflight >= allowed {
		return false, BusyWait
	}
	s.inflight++
	return true, 0
}

// Release ends an attempt that [Lockouts.Acquire] admitted, with its outcome. It returns the lockout that a failure
// started, if any.
func (l *Lockouts) Release(key string, outcome Outcome) Lock {
	if l.policy.Threshold <= 0 {
		return Lock{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t := l.now()
	s, found := l.table.get(key)
	if !found {
		if outcome != Failure {
			return Lock{}
		}
		s = &lockState{}
		l.table.put(key, s, func(s *lockState) bool { return s.inflight > 0 })
	}
	if s.inflight > 0 {
		s.inflight--
	}
	var lock Lock
	switch outcome {
	case Success:
		s.failures = 0
		s.lockedUntil = time.Time{}
	case Failure:
		l.forget(s, t)
		s.failures++
		s.lastFailure = t
		if s.failures >= l.policy.Threshold {
			d := l.policy.lockout(s.failures)
			if until := t.Add(d); until.After(s.lockedUntil) {
				s.lockedUntil = until
			}
			lock = Lock{Duration: d, First: s.failures == l.policy.Threshold, Failures: s.failures}
		}
	}
	if l.idle(s, t) {
		l.table.remove(key)
	}
	return lock
}

// Len returns the number of keys held.
func (l *Lockouts) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.table.len()
}

// Rate configures [Budgets]: a key may make Burst requests at once, and gains one more every Every, up to Burst. A
// Burst of zero or less disables the budget.
type Rate struct {
	Burst int
	Every time.Duration
}

type bucket struct {
	tokens float64
	at     time.Time
}

// Budgets limits the requests per key with token buckets (see [Rate]).
type Budgets struct {
	mu    sync.Mutex
	rate  Rate
	now   func() time.Time
	table table[*bucket]
}

// NewBudgets returns request budgets with rate, holding at most capacity keys. now is the clock; nil means time.Now.
func NewBudgets(rate Rate, capacity int, now func() time.Time) *Budgets {
	if now == nil {
		now = time.Now
	}
	return &Budgets{rate: rate, now: now, table: newTable[*bucket](capacity)}
}

// refill brings b's tokens up to date at t.
func (b *Budgets) refill(bk *bucket, t time.Time) {
	if elapsed := t.Sub(bk.at); elapsed > 0 && b.rate.Every > 0 {
		bk.tokens = min(float64(b.rate.Burst), bk.tokens+float64(elapsed)/float64(b.rate.Every))
	}
	bk.at = t
}

// Take uses one request of key's budget, or reports how long until the next one is available.
func (b *Budgets) Take(key string) (ok bool, wait time.Duration) {
	if b.rate.Burst <= 0 {
		return true, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.now()
	b.table.sweep(func(bk *bucket) bool {
		b.refill(bk, t)
		return bk.tokens >= float64(b.rate.Burst)
	})
	bk, found := b.table.get(key)
	if !found {
		bk = &bucket{tokens: float64(b.rate.Burst), at: t}
		b.table.put(key, bk, nil)
	}
	b.refill(bk, t)
	if bk.tokens < 1 {
		if b.rate.Every <= 0 {
			return false, time.Hour
		}
		return false, time.Duration((1 - bk.tokens) * float64(b.rate.Every))
	}
	bk.tokens--
	return true, 0
}

// Len returns the number of keys held.
func (b *Budgets) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.table.len()
}

// table is a map with a capacity, in least recently used order. It is not safe for concurrent use.
type table[V any] struct {
	capacity int
	entries  map[string]*list.Element
	order    *list.List // front: most recently used
}

type entry[V any] struct {
	key   string
	value V
}

// sweepPerCall bounds the entries [table.sweep] looks at.
const sweepPerCall = 2

func newTable[V any](capacity int) table[V] {
	return table[V]{capacity: max(1, capacity), entries: map[string]*list.Element{}, order: list.New()}
}

func (t *table[V]) len() int { return len(t.entries) }

// get returns the value of key and marks it as used.
func (t *table[V]) get(key string) (V, bool) {
	el, ok := t.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	t.order.MoveToFront(el)
	return el.Value.(*entry[V]).value, true
}

// put adds key, which must not be present. When the table is full, the least recently used entries are dropped,
// except those for which keep reports true; if every entry is kept, the table grows beyond its capacity (keep is
// meant for attempts in flight, which concurrency bounds).
func (t *table[V]) put(key string, v V, keep func(V) bool) {
	for el := t.order.Back(); len(t.entries) >= t.capacity && el != nil; {
		prev := el.Prev()
		e := el.Value.(*entry[V])
		if keep == nil || !keep(e.value) {
			t.order.Remove(el)
			delete(t.entries, e.key)
		}
		el = prev
	}
	t.entries[key] = t.order.PushFront(&entry[V]{key: key, value: v})
}

func (t *table[V]) remove(key string) {
	if el, ok := t.entries[key]; ok {
		t.order.Remove(el)
		delete(t.entries, key)
	}
}

// sweep removes the least recently used entries, a few at most, for which done reports that they carry nothing.
func (t *table[V]) sweep(done func(V) bool) {
	for i := 0; i < sweepPerCall; i++ {
		el := t.order.Back()
		if el == nil {
			return
		}
		e := el.Value.(*entry[V])
		if !done(e.value) {
			return
		}
		t.order.Remove(el)
		delete(t.entries, e.key)
	}
}
