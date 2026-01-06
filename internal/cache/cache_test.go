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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func TestCache_SetAndGet(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	key := "test/key"
	value := types.NamespacedName{Namespace: "ns", Name: "name"}

	// Key should not exist initially
	_, found := c.Get(key)
	assert.False(t, found)

	// Set and retrieve
	c.SetPersistent(key, value)
	got, found := c.Get(key)
	assert.True(t, found)
	assert.Equal(t, value, got)
}

func TestCache_SetWithTTL(t *testing.T) {
	c := NewCache(100 * time.Millisecond)
	defer c.Stop()

	key := "test/key"
	value := types.NamespacedName{Namespace: "ns", Name: "name"}

	// Set with short TTL
	c.SetWithTTL(key, value, 200*time.Millisecond)

	// Should exist immediately
	_, found := c.Get(key)
	assert.True(t, found)

	// Wait for expiration + cleanup
	time.Sleep(400 * time.Millisecond)

	// Should be expired
	_, found = c.Get(key)
	assert.False(t, found)
}

func TestCache_SetPersistent(t *testing.T) {
	c := NewCache(100 * time.Millisecond)
	defer c.Stop()

	key := "test/key"
	value := types.NamespacedName{Namespace: "ns", Name: "name"}

	c.SetPersistent(key, value)

	// Wait for multiple cleanup cycles
	time.Sleep(300 * time.Millisecond)

	// Should still exist (persistent entries don't expire)
	got, found := c.Get(key)
	assert.True(t, found)
	assert.Equal(t, value, got)
}

func TestCache_Delete(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	key := "test/key"
	value := types.NamespacedName{Namespace: "ns", Name: "name"}

	c.SetPersistent(key, value)
	assert.True(t, c.Exists(key))

	c.Delete(key)
	assert.False(t, c.Exists(key))
}

func TestCache_Exists(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	key := "test/key"
	value := types.NamespacedName{Namespace: "ns", Name: "name"}

	assert.False(t, c.Exists(key))

	c.SetPersistent(key, value)
	assert.True(t, c.Exists(key))
}

func TestCache_IsPersistent(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	key1 := "persistent/key"
	key2 := "ttl/key"
	value := types.NamespacedName{Namespace: "ns", Name: "name"}

	// Non-existent key
	result := c.IsPersistent("nonexistent")
	assert.Nil(t, result)

	// Persistent key
	c.SetPersistent(key1, value)
	result = c.IsPersistent(key1)
	require.NotNil(t, result)
	assert.True(t, *result)

	// TTL key
	c.SetWithTTL(key2, value, time.Hour)
	result = c.IsPersistent(key2)
	require.NotNil(t, result)
	assert.False(t, *result)
}

func TestCache_Len(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	assert.Equal(t, 0, c.Len())

	c.SetPersistent("key1", types.NamespacedName{Namespace: "ns", Name: "name1"})
	assert.Equal(t, 1, c.Len())

	c.SetPersistent("key2", types.NamespacedName{Namespace: "ns", Name: "name2"})
	assert.Equal(t, 2, c.Len())

	c.Delete("key1")
	assert.Equal(t, 1, c.Len())
}

func TestCache_Clear(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	c.SetPersistent("key1", types.NamespacedName{Namespace: "ns", Name: "name1"})
	c.SetPersistent("key2", types.NamespacedName{Namespace: "ns", Name: "name2"})
	assert.Equal(t, 2, c.Len())

	c.Clear()
	assert.Equal(t, 0, c.Len())
}

func TestCache_Keys(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	c.SetPersistent("key1", types.NamespacedName{Namespace: "ns", Name: "name1"})
	c.SetPersistent("key2", types.NamespacedName{Namespace: "ns", Name: "name2"})

	keys := c.Keys()
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "key1")
	assert.Contains(t, keys, "key2")
}

func TestCache_Stop(t *testing.T) {
	c := NewCache(time.Millisecond)

	// Should not hang
	done := make(chan struct{})
	go func() {
		c.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Stop() did not complete in time")
	}
}

func TestCache_StopWithContext(t *testing.T) {
	c := NewCache(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := c.StopWithContext(ctx)
	assert.NoError(t, err)
}

func TestCache_ConcurrentAccess(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	const goroutines = 100
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				key := "test/key"
				value := types.NamespacedName{Namespace: "ns", Name: "name"}

				c.SetPersistent(key, value)
				c.Get(key)
				c.Exists(key)
				c.IsPersistent(key)
			}
		}(i)
	}

	wg.Wait()
}

func TestCache_CleanupRemovesExpiredEntries(t *testing.T) {
	c := NewCache(50 * time.Millisecond)
	defer c.Stop()

	// Add a mix of persistent and TTL entries
	c.SetPersistent("persistent", types.NamespacedName{Namespace: "ns", Name: "persistent"})
	c.SetWithTTL("short", types.NamespacedName{Namespace: "ns", Name: "short"}, 100*time.Millisecond)
	c.SetWithTTL("long", types.NamespacedName{Namespace: "ns", Name: "long"}, time.Hour)

	// Wait for short TTL to expire and cleanup to run
	time.Sleep(200 * time.Millisecond)

	assert.True(t, c.Exists("persistent"), "persistent should exist")
	assert.False(t, c.Exists("short"), "short should be expired")
	assert.True(t, c.Exists("long"), "long should exist")
}

func TestCache_GetReturnsExpiredAsFalse(t *testing.T) {
	c := NewCache(time.Hour) // Long cleanup interval
	defer c.Stop()

	key := "test/key"
	c.SetWithTTL(key, types.NamespacedName{Namespace: "ns", Name: "name"}, 10*time.Millisecond)

	// Should exist initially
	_, found := c.Get(key)
	assert.True(t, found)

	// Wait for expiration
	time.Sleep(50 * time.Millisecond)

	// Should report as not found (expired)
	_, found = c.Get(key)
	assert.False(t, found)
}

func TestCache_OverwriteEntry(t *testing.T) {
	c := NewCache(time.Hour)
	defer c.Stop()

	key := "test/key"
	value1 := types.NamespacedName{Namespace: "ns1", Name: "name1"}
	value2 := types.NamespacedName{Namespace: "ns2", Name: "name2"}

	c.SetPersistent(key, value1)
	got, _ := c.Get(key)
	assert.Equal(t, value1, got)

	c.SetPersistent(key, value2)
	got, _ = c.Get(key)
	assert.Equal(t, value2, got)
}

// Benchmarks

func BenchmarkCache_Set(b *testing.B) {
	c := NewCache(time.Hour)
	defer c.Stop()

	value := types.NamespacedName{Namespace: "ns", Name: "name"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.SetPersistent("test/key", value)
	}
}

func BenchmarkCache_Get(b *testing.B) {
	c := NewCache(time.Hour)
	defer c.Stop()

	c.SetPersistent("test/key", types.NamespacedName{Namespace: "ns", Name: "name"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get("test/key")
	}
}

func BenchmarkCache_ConcurrentReadWrite(b *testing.B) {
	c := NewCache(time.Hour)
	defer c.Stop()

	value := types.NamespacedName{Namespace: "ns", Name: "name"}
	c.SetPersistent("test/key", value)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%2 == 0 {
				c.Get("test/key")
			} else {
				c.SetPersistent("test/key", value)
			}
			i++
		}
	})
}

func BenchmarkCache_Exists(b *testing.B) {
	c := NewCache(time.Hour)
	defer c.Stop()

	c.SetPersistent("test/key", types.NamespacedName{Namespace: "ns", Name: "name"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Exists("test/key")
	}
}

