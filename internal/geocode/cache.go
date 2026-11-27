package geocode

import (
	"container/list"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/unicode/norm"
)

// cache keeps answers in memory, least recently used first out, each for a fixed time. Only answers are kept, never
// failures. It holds search terms, so it never leaves memory: not logged, not written to the database (M2-Q13).
type cache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	now      func() time.Time
	entries  map[string]*list.Element
	order    *list.List // front: most recently used
}

type cacheEntry struct {
	key     string
	results []Result
	expires time.Time
}

func newCache(capacity int, ttl time.Duration, now func() time.Time) *cache {
	return &cache{capacity: capacity, ttl: ttl, now: now, entries: map[string]*list.Element{}, order: list.New()}
}

// cacheKey is the search term folded (NFKC, lower case, spaces collapsed) with the bounding box: searches that differ
// only in case or spacing share an answer.
func cacheKey(q Query) string {
	text := strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(q.Text))), " ")
	return q.BBox.String() + "\x00" + text
}

func (c *cache) get(key string) ([]Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*cacheEntry)
	if !c.now().Before(e.expires) {
		c.order.Remove(el)
		delete(c.entries, key)
		return nil, false
	}
	c.order.MoveToFront(el)
	return clone(e.results), true
}

func (c *cache) put(key string, results []Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.Remove(el)
		delete(c.entries, key)
	}
	for len(c.entries) >= c.capacity {
		el := c.order.Back()
		c.order.Remove(el)
		delete(c.entries, el.Value.(*cacheEntry).key)
	}
	c.entries[key] = c.order.PushFront(&cacheEntry{key: key, results: clone(results), expires: c.now().Add(c.ttl)})
}

// len returns the number of answers held.
func (c *cache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}
