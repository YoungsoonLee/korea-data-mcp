// Package cache is a small in-memory TTL cache. Most Korean public/financial
// data changes daily at most, and upstream quotas are tight (e.g. OpenDART
// 20,000 calls/day), so caching identical requests matters.
package cache

import (
	"sync"
	"time"
)

type entry[V any] struct {
	value   V
	expires time.Time
}

type Cache[V any] struct {
	mu         sync.Mutex
	items      map[string]entry[V]
	maxEntries int
	now        func() time.Time
}

func New[V any](maxEntries int) *Cache[V] {
	return &Cache[V]{items: make(map[string]entry[V]), maxEntries: maxEntries, now: time.Now}
}

func (c *Cache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok || c.now().After(e.expires) {
		delete(c.items, key)
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *Cache[V]) Set(key string, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.items[key]; !exists && len(c.items) >= c.maxEntries {
		c.evictLocked()
	}
	c.items[key] = entry[V]{value: value, expires: c.now().Add(ttl)}
}

// evictLocked drops expired entries, or one arbitrary entry if none expired.
func (c *Cache[V]) evictLocked() {
	now := c.now()
	for k, e := range c.items {
		if now.After(e.expires) {
			delete(c.items, k)
		}
	}
	if len(c.items) < c.maxEntries {
		return
	}
	for k := range c.items {
		delete(c.items, k)
		return
	}
}
