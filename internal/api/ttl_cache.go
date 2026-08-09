package api

import (
	"sync"
	"time"
)

// expiringEntry is a single expiringCache entry: the value plus the moment it
// was stored.
type expiringEntry[V any] struct {
	value     V
	createdAt time.Time
}

// expiringCache is a generic key-value cache whose entries expire ttl after
// being set. Expiration is lazy: every set schedules a cleanup timer and
// get/pop also drop stale entries when they are touched. The createdAt guard
// on the cleanup keeps a re-set value alive until its own TTL elapses, even if
// an older cleanup timer is still pending for the key.
type expiringCache[V any] struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]expiringEntry[V]
}

func newExpiringCache[V any](ttl time.Duration) *expiringCache[V] {
	return &expiringCache[V]{
		ttl:     ttl,
		entries: make(map[string]expiringEntry[V]),
	}
}

// set stores v under key and schedules a lazy cleanup that removes the entry
// once ttl has elapsed since this write.
func (c *expiringCache[V]) set(key string, v V) {
	now := time.Now()
	c.mu.Lock()
	c.entries[key] = expiringEntry[V]{value: v, createdAt: now}
	c.mu.Unlock()
	time.AfterFunc(c.ttl, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if entry, exists := c.entries[key]; exists && time.Since(entry.createdAt) >= c.ttl {
			delete(c.entries, key)
		}
	})
}

// get returns the value stored under key. Stale entries are removed and
// reported as missing.
func (c *expiringCache[V]) get(key string) (V, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		var zero V
		return zero, false
	}
	if time.Since(entry.createdAt) < c.ttl {
		return entry.value, true
	}
	// Remove the stale entry under the write lock. Re-read it in case another
	// goroutine refreshed the key between the read lock and this one.
	c.mu.Lock()
	if entry, exists := c.entries[key]; exists && time.Since(entry.createdAt) >= c.ttl {
		delete(c.entries, key)
	}
	c.mu.Unlock()
	var zero V
	return zero, false
}

// pop atomically reads and deletes the value under key. It replaces the
// read-then-delete pattern used by the consume-once flows (an import consumes
// the cached chapter list exactly once).
func (c *expiringCache[V]) pop(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	delete(c.entries, key)
	if time.Since(entry.createdAt) >= c.ttl {
		var zero V
		return zero, false
	}
	return entry.value, true
}
