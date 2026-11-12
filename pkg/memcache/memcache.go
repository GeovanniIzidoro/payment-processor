// Package memcache provides a simple in-memory cache with a default TTL for items.
package memcache

import (
	"hash/fnv"
	"sync"
	"time"
)

const shardCount = 16

type entry[V any] struct {
	value     V
	expiresAt time.Time
}

type shard[V any] struct {
	mu    sync.RWMutex
	items map[string]entry[V]
}

// Cache is a thread-safe in-memory cache with a default TTL for items.
type Cache[V any] struct {
	shards [shardCount]*shard[V]
	ttl    time.Duration
	now    func() time.Time
	stop   chan struct{}
	once   sync.Once
}

// New creates a new Cache with the specified default TTL and sweep interval.
func New[V any](ttl, sweepEvery time.Duration) *Cache[V] {
	c := &Cache[V]{
		ttl:  ttl,
		now:  time.Now,
		stop: make(chan struct{}),
	}

	for i := range c.shards {
		c.shards[i] = &shard[V]{items: make(map[string]entry[V])}
	}

	go c.janitor(sweepEvery)
	return c
}

// shardFor returns the shard corresponding to the given key.
func (c *Cache[V]) shardFor(key string) *shard[V] {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return c.shards[h.Sum32()%shardCount]
}

// SetTTL sets the value for the key with a specific TTL.
func (c *Cache[V]) SetTTL(key string, value V, ttl time.Duration) {
	s := c.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items[key] = entry[V]{value: value, expiresAt: c.now().Add(ttl)}
}

// Get returns the value associated with the key, or false if the key is not present or has expired.
func (c *Cache[V]) Get(key string) (V, bool) {
	s := c.shardFor(key)
	s.mu.RLock()
	v, ok := s.items[key]
	s.mu.RUnlock()

	if !ok || c.now().After(v.expiresAt) {
		var zero V
		return zero, false
	}
	return v.value, true
}

func (c *Cache[V]) Set(key string, value V) {
	c.SetTTL(key, value, c.ttl)
}

// Increment adds delta to an integer counter stored at key and returns
// the new total. Counters share the default TTL, refresed on write.
// It is the primitive behind velocity cheks.
func (c *Cache[V]) Increment(key string, counter func(current V, exists bool) V) V {
	s := c.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.items[key]
	valid := ok && c.now().Before(e.expiresAt)

	next := counter(e.value, valid)
	s.items[key] = entry[V]{value: next, expiresAt: c.now().Add(c.ttl)}
	return next
}

// Len returns the number of items in the cache.
func (c *Cache[V]) Len() int {
	var total int
	for _, s := range c.shards {
		s.mu.RLock()
		total += len(s.items)
		s.mu.RUnlock()
	}
	return total
}

// Close stops the janitor goroutine and releases resources.
func (c *Cache[V]) Close() error {
	c.once.Do(func() { close(c.stop) })
	return nil
}

func (c *Cache[V]) janitor(every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			now := c.now()
			for _, s := range c.shards {
				s.mu.Lock()
				for k, v := range s.items {
					if now.After(v.expiresAt) {
						delete(s.items, k)
					}
				}
				s.mu.Unlock()
			}
		}
	}
}
