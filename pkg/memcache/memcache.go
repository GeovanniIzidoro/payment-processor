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

type Cache[V any] struct {
	shards [shardCount]*shard[V]
	ttl    time.Duration
	now    func() time.Time
	stop   chan struct{}
	once   sync.Once
}

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

func (c *Cache[V]) janitor(every time.Duration) {}

func (c *Cache[V]) shardFor(key string) *shard[V] {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return c.shards[h.Sum32()%shardCount]
}

func (c *Cache[V]) SetTTL(key string, value V, ttl time.Duration) {
	s  := c.shardFor(key)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items[key] = entry[V]{ value: value, expiresAt: c.now().Add(ttl)}
}

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