package ratelimit_test

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EatWell-Studio/OSFM/internal/ratelimit"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_800_000_000, 0)} }

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

var policy = ratelimit.Policy{Threshold: 3, Lockout: time.Minute, MaxLockout: 8 * time.Minute, Forget: time.Hour}

// fail makes one failed attempt and returns the lockout it started; it fails the test if the attempt is refused.
func fail(t *testing.T, l *ratelimit.Lockouts, key string) ratelimit.Lock {
	t.Helper()
	if ok, wait := l.Acquire(key); !ok {
		t.Fatalf("attempt for %q refused, wait %v", key, wait)
	}
	return l.Release(key, ratelimit.Failure)
}

func TestLockoutDoublesUpToTheMaximum(t *testing.T) {
	c := newClock()
	l := ratelimit.NewLockouts(policy, 100, c.now)
	for i := 1; i < policy.Threshold; i++ {
		if lock := fail(t, l, "k"); lock.Duration != 0 {
			t.Fatalf("failure %d locked the key for %v", i, lock.Duration)
		}
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 8 * time.Minute}
	for i, d := range want {
		lock := fail(t, l, "k")
		if lock.Duration != d || lock.First != (i == 0) {
			t.Fatalf("lockout %d: %+v, want %v (first: %v)", i, lock, d, i == 0)
		}
		if ok, wait := l.Acquire("k"); ok || wait != d {
			t.Fatalf("during lockout %d: admitted %v, wait %v, want %v", i, ok, wait, d)
		}
		c.advance(d - time.Second)
		if ok, _ := l.Acquire("k"); ok {
			t.Fatalf("admitted a second before lockout %d ends", i)
		}
		c.advance(time.Second)
	}
	// Other keys are not affected.
	if ok, _ := l.Acquire("other"); !ok {
		t.Fatal("another key is locked too")
	}
	l.Release("other", ratelimit.Neutral)
}

func TestOneAttemptAtATimeAfterALockout(t *testing.T) {
	c := newClock()
	l := ratelimit.NewLockouts(policy, 100, c.now)
	for range policy.Threshold {
		fail(t, l, "k")
	}
	c.advance(time.Minute)
	if ok, _ := l.Acquire("k"); !ok {
		t.Fatal("refused after the lockout ended")
	}
	if ok, wait := l.Acquire("k"); ok || wait != ratelimit.BusyWait {
		t.Fatalf("second concurrent attempt: admitted %v, wait %v", ok, wait)
	}
	l.Release("k", ratelimit.Neutral)
	if ok, _ := l.Acquire("k"); !ok {
		t.Fatal("refused after the attempt in flight ended")
	}
	l.Release("k", ratelimit.Neutral)
}

func TestSuccessAndTimeForgetFailures(t *testing.T) {
	c := newClock()
	l := ratelimit.NewLockouts(policy, 100, c.now)
	fail(t, l, "k")
	fail(t, l, "k")
	l.Acquire("k")
	l.Release("k", ratelimit.Success)
	if l.Len() != 0 {
		t.Errorf("%d keys held after a success, want 0", l.Len())
	}
	fail(t, l, "k")
	fail(t, l, "k")
	if lock := fail(t, l, "k"); lock.Duration == 0 {
		t.Fatal("the counter did not count again after the success")
	}

	// Neutral outcomes change nothing; failures are forgotten after Forget without a failure.
	fail(t, l, "j")
	fail(t, l, "j")
	l.Acquire("j")
	l.Release("j", ratelimit.Neutral)
	c.advance(policy.Forget)
	if lock := fail(t, l, "j"); lock.Duration != 0 {
		t.Fatalf("failures were not forgotten after %v: %+v", policy.Forget, lock)
	}
}

// TestConcurrentAttemptsCannotPassTheThreshold starts many attempts for one key at once: no more than the threshold
// are admitted before their outcomes are known, so racing does not buy extra tries.
func TestConcurrentAttemptsCannotPassTheThreshold(t *testing.T) {
	l := ratelimit.NewLockouts(ratelimit.Policy{Threshold: 5, Lockout: time.Hour, MaxLockout: time.Hour,
		Forget: 24 * time.Hour}, 100, nil)
	var admitted atomic.Int32
	var start, wg sync.WaitGroup
	start.Add(1)
	release := make(chan struct{})
	for range 100 {
		wg.Go(func() {
			start.Wait()
			if ok, _ := l.Acquire("k"); ok {
				admitted.Add(1)
				<-release
				l.Release("k", ratelimit.Failure)
			}
		})
	}
	start.Done()
	// Every goroutine has either been refused or is holding its attempt once the count stops changing; waiting for
	// the refusals is enough, as admitted attempts block on release.
	deadline := time.Now().Add(5 * time.Second)
	for admitted.Load() < 5 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()
	if n := admitted.Load(); n != 5 {
		t.Fatalf("%d attempts admitted, want 5", n)
	}
	if ok, _ := l.Acquire("k"); ok {
		t.Fatal("the key is not locked after 5 failures")
	}

	// The same with outcomes reported as they come: still never more than 5 failures before the lockout.
	l2 := ratelimit.NewLockouts(ratelimit.Policy{Threshold: 5, Lockout: time.Hour, MaxLockout: time.Hour,
		Forget: 24 * time.Hour}, 100, nil)
	var failures atomic.Int32
	for range 200 {
		wg.Go(func() {
			if ok, _ := l2.Acquire("k"); ok {
				failures.Add(1)
				l2.Release("k", ratelimit.Failure)
			}
		})
	}
	wg.Wait()
	if n := failures.Load(); n != 5 {
		t.Fatalf("%d failures admitted, want 5", n)
	}
}

// TestMemoryIsBounded fails with many distinct keys: the tables never hold more than their capacity.
func TestMemoryIsBounded(t *testing.T) {
	const capacity = 1000
	l := ratelimit.NewLockouts(policy, capacity, nil)
	b := ratelimit.NewBudgets(ratelimit.Rate{Burst: 2, Every: time.Minute}, capacity, nil)
	for i := range 100 * capacity {
		key := strconv.Itoa(i)
		fail(t, l, key)
		b.Take(key)
		if l.Len() > capacity || b.Len() > capacity {
			t.Fatalf("after %d keys: %d and %d held, capacity %d", i+1, l.Len(), b.Len(), capacity)
		}
	}
	// The most recent keys are still there.
	fail(t, l, "99999")
	if lock := fail(t, l, "99999"); lock.Duration == 0 {
		t.Error("the most recently used key lost its failures")
	}
}

// TestKeysInFlightAreNotEvicted: an attempt in flight keeps its entry, so its outcome is still counted.
func TestKeysInFlightAreNotEvicted(t *testing.T) {
	l := ratelimit.NewLockouts(ratelimit.Policy{Threshold: 1, Lockout: time.Hour, MaxLockout: time.Hour,
		Forget: 24 * time.Hour}, 2, nil)
	if ok, _ := l.Acquire("held"); !ok {
		t.Fatal("refused")
	}
	for i := range 10 {
		fail(t, l, strconv.Itoa(i))
	}
	if lock := l.Release("held", ratelimit.Failure); lock.Duration == 0 {
		t.Fatal("the failure of an attempt in flight was lost")
	}
	if ok, _ := l.Acquire("held"); ok {
		t.Fatal("the key is not locked")
	}
}

func TestBudgets(t *testing.T) {
	c := newClock()
	b := ratelimit.NewBudgets(ratelimit.Rate{Burst: 3, Every: 10 * time.Second}, 100, c.now)
	for i := range 3 {
		if ok, _ := b.Take("k"); !ok {
			t.Fatalf("request %d refused", i)
		}
	}
	if ok, wait := b.Take("k"); ok || wait != 10*time.Second {
		t.Fatalf("over the burst: admitted %v, wait %v", ok, wait)
	}
	c.advance(5 * time.Second)
	if ok, wait := b.Take("k"); ok || wait != 5*time.Second {
		t.Fatalf("half refilled: admitted %v, wait %v", ok, wait)
	}
	c.advance(5 * time.Second)
	if ok, _ := b.Take("k"); !ok {
		t.Fatal("refused after refilling one request")
	}
	if ok, _ := b.Take("other"); !ok {
		t.Fatal("another key has no budget")
	}
	// Full buckets are dropped as they are found.
	c.advance(time.Hour)
	b.Take("x")
	b.Take("y")
	if b.Len() > 2 {
		t.Errorf("%d keys held after an hour, want the full buckets dropped", b.Len())
	}
}

func TestBudgetsAreSafeForConcurrentUse(t *testing.T) {
	b := ratelimit.NewBudgets(ratelimit.Rate{Burst: 10, Every: time.Hour}, 100, nil)
	var taken atomic.Int32
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if ok, _ := b.Take("k"); ok {
				taken.Add(1)
			}
		})
	}
	wg.Wait()
	if n := taken.Load(); n != 10 {
		t.Fatalf("%d requests admitted, want 10", n)
	}
}

func TestDisabledPolicies(t *testing.T) {
	l := ratelimit.NewLockouts(ratelimit.Policy{}, 10, nil)
	b := ratelimit.NewBudgets(ratelimit.Rate{}, 10, nil)
	for range 100 {
		if ok, _ := l.Acquire("k"); !ok {
			t.Fatal("a disabled policy refused")
		}
		l.Release("k", ratelimit.Failure)
		if ok, _ := b.Take("k"); !ok {
			t.Fatal("a disabled budget refused")
		}
	}
}
