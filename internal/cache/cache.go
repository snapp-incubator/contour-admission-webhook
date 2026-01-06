// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cache

import (
	"context"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var logger = ctrl.Log.WithName("cache")

// Cache provides a thread-safe in-memory cache with TTL-based expiration.
// It stores FQDN ownership information for HTTPProxy resources.
type Cache struct {
	entries         map[string]*entry
	mu              sync.RWMutex
	cleanupInterval time.Duration
	stopCh          chan struct{}
	stopped         chan struct{}
}

// entry represents a cache entry with optional expiration.
type entry struct {
	Value     types.NamespacedName
	ExpiresAt time.Time // Zero value means no expiration (persistent)
}

// isPersistent returns true if this entry never expires.
func (e *entry) isPersistent() bool {
	return e.ExpiresAt.IsZero()
}

// isExpired returns true if this entry has expired.
func (e *entry) isExpired(now time.Time) bool {
	return !e.isPersistent() && now.After(e.ExpiresAt)
}

// NewCache creates a new cache with the specified cleanup interval.
// The cleanup goroutine runs periodically to remove expired entries.
func NewCache(cleanupInterval time.Duration) *Cache {
	c := &Cache{
		entries:         make(map[string]*entry),
		cleanupInterval: cleanupInterval,
		stopCh:          make(chan struct{}),
		stopped:         make(chan struct{}),
	}

	go c.runCleanupLoop()

	return c
}

// Set stores a value with an optional expiration time.
// If expiresAt is zero, the entry is persistent (never expires).
func (c *Cache) Set(key string, value types.NamespacedName, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &entry{
		Value:     value,
		ExpiresAt: expiresAt,
	}
}

// SetWithTTL stores a value that expires after the specified duration.
// If ttl is zero or negative, the entry is persistent.
func (c *Cache) SetWithTTL(key string, value types.NamespacedName, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	c.Set(key, value, expiresAt)
}

// SetPersistent stores a value that never expires.
func (c *Cache) SetPersistent(key string, value types.NamespacedName) {
	c.Set(key, value, time.Time{})
}

// Get retrieves a value from the cache.
// Returns the value and true if found, zero value and false otherwise.
func (c *Cache) Get(key string) (types.NamespacedName, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, found := c.entries[key]
	if !found {
		return types.NamespacedName{}, false
	}

	// Check if expired (don't delete here to avoid write lock upgrade)
	if e.isExpired(time.Now()) {
		return types.NamespacedName{}, false
	}

	return e.Value, true
}

// Delete removes an entry from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
}

// Exists returns true if the key exists and is not expired.
func (c *Cache) Exists(key string) bool {
	_, found := c.Get(key)
	return found
}

// IsPersistent returns nil if key doesn't exist, otherwise returns whether the entry is persistent.
func (c *Cache) IsPersistent(key string) *bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	e, found := c.entries[key]
	if !found || e.isExpired(time.Now()) {
		return nil
	}

	persistent := e.isPersistent()
	return &persistent
}

// Len returns the number of entries in the cache (including potentially expired ones).
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.entries)
}

// Stop gracefully stops the cleanup goroutine.
// It blocks until the cleanup goroutine has stopped.
func (c *Cache) Stop() {
	close(c.stopCh)
	<-c.stopped
}

// StopWithContext stops the cleanup goroutine with a context for timeout.
func (c *Cache) StopWithContext(ctx context.Context) error {
	close(c.stopCh)

	select {
	case <-c.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runCleanupLoop periodically removes expired entries.
func (c *Cache) runCleanupLoop() {
	defer close(c.stopped)

	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanup()
		case <-c.stopCh:
			return
		}
	}
}

// cleanup removes all expired entries from the cache.
func (c *Cache) cleanup() {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, e := range c.entries {
		if e.isExpired(now) {
			delete(c.entries, key)
			logger.V(1).Info("cache entry expired and deleted", "key", key)
		}
	}
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*entry)
}

// Keys returns all non-expired keys in the cache.
func (c *Cache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	keys := make([]string, 0, len(c.entries))

	for key, e := range c.entries {
		if !e.isExpired(now) {
			keys = append(keys, key)
		}
	}

	return keys
}
