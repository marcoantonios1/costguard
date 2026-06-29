package cache

import (
	"testing"
	"time"
)

func freshEntry(body string, ttl time.Duration) Entry {
	return Entry{
		StatusCode: 200,
		Body:       []byte(body),
		ExpiresAt:  time.Now().Add(ttl),
	}
}

func expiredEntry(body string) Entry {
	return Entry{
		StatusCode: 200,
		Body:       []byte(body),
		ExpiresAt:  time.Now().Add(-time.Second),
	}
}

// TestGet_ExpiredEntry_Deleted verifies the normal expiry path: a stale entry
// is deleted from the map and Get returns (Entry{}, false).
func TestGet_ExpiredEntry_Deleted(t *testing.T) {
	m := NewMemory(100)
	m.Set("k", expiredEntry("stale"))

	entry, ok := m.Get("k")
	if ok {
		t.Fatalf("expected cache miss for expired entry, got hit: %q", entry.Body)
	}

	// Key must have been removed.
	m.mu.RLock()
	_, stillPresent := m.entries["k"]
	m.mu.RUnlock()
	if stillPresent {
		t.Error("expired key must be deleted from the map after Get")
	}
}

// TestGet_ConcurrentSet_FreshEntryNotEvicted models the race window between
// Get's RUnlock and Lock: a concurrent Set writes a fresh entry for the same
// key after Get has already read the expired entry under the read lock.
//
// Without the re-check under the write lock, Get would delete the fresh entry
// and return a miss. With the fix, Get must return the fresh entry as a hit.
func TestGet_ConcurrentSet_FreshEntryNotEvicted(t *testing.T) {
	m := NewMemory(100)

	// Seed an already-expired entry.
	m.Set("k", expiredEntry("stale"))

	// Simulate the concurrent Set that lands between Get's RUnlock and Lock:
	// overwrite with a fresh, non-expired entry before calling Get.
	m.Set("k", freshEntry("fresh", time.Minute))

	entry, ok := m.Get("k")
	if !ok {
		t.Fatal("expected cache hit for fresh entry written concurrently, got miss")
	}
	if string(entry.Body) != "fresh" {
		t.Errorf("body: got %q, want %q", entry.Body, "fresh")
	}

	// Fresh entry must still be in the map — Get must not have deleted it.
	m.mu.RLock()
	current, present := m.entries["k"]
	m.mu.RUnlock()
	if !present {
		t.Error("fresh entry was deleted from the map by Get — should have been kept")
	}
	if present && string(current.Body) != "fresh" {
		t.Errorf("map body: got %q, want %q", current.Body, "fresh")
	}
}

// TestGet_KeyDeletedByThirdGoroutine verifies that if another goroutine deletes
// the key entirely between Get's RUnlock and Lock, Get handles the missing key
// gracefully and returns (Entry{}, false).
func TestGet_KeyDeletedByThirdGoroutine(t *testing.T) {
	m := NewMemory(100)
	m.Set("k", expiredEntry("stale"))

	// Simulate a concurrent delete of the key (e.g. by another Get that won
	// the write lock first).
	m.mu.Lock()
	delete(m.entries, "k")
	m.mu.Unlock()

	// Get should not panic and should return a miss.
	entry, ok := m.Get("k")
	if ok {
		t.Fatalf("expected miss for deleted key, got hit: %q", entry.Body)
	}
}

// TestGet_UnexpiredEntry_Hit verifies the normal hit path is unaffected.
func TestGet_UnexpiredEntry_Hit(t *testing.T) {
	m := NewMemory(100)
	m.Set("k", freshEntry("hello", time.Minute))

	entry, ok := m.Get("k")
	if !ok {
		t.Fatal("expected hit for valid entry, got miss")
	}
	if string(entry.Body) != "hello" {
		t.Errorf("body: got %q, want %q", entry.Body, "hello")
	}
}

// ---------------------------------------------------------------------------
// Eviction tests (Set)
// ---------------------------------------------------------------------------

// TestSet_Eviction_SweepsAllExpiredBeforeLive verifies that when the cache is
// full with a mix of expired and live entries, Set removes all expired entries
// and leaves every live entry untouched.
func TestSet_Eviction_SweepsAllExpiredBeforeLive(t *testing.T) {
	m := NewMemory(3)

	m.Set("exp1", expiredEntry("stale-1"))
	m.Set("exp2", expiredEntry("stale-2"))
	m.Set("live", freshEntry("hot", time.Minute))
	// Cache is now full (3/3): exp1, exp2, live.

	m.Set("new", freshEntry("new-value", time.Minute))

	// Both expired entries must be gone.
	m.mu.RLock()
	_, hasExp1 := m.entries["exp1"]
	_, hasExp2 := m.entries["exp2"]
	liveEntry, hasLive := m.entries["live"]
	newEntry, hasNew := m.entries["new"]
	m.mu.RUnlock()

	if hasExp1 {
		t.Error("exp1 (expired) should have been evicted")
	}
	if hasExp2 {
		t.Error("exp2 (expired) should have been evicted")
	}
	if !hasLive {
		t.Error("live entry must not be evicted while expired entries existed")
	} else if string(liveEntry.Body) != "hot" {
		t.Errorf("live entry body: got %q, want %q", liveEntry.Body, "hot")
	}
	if !hasNew {
		t.Error("new entry must be present after Set")
	} else if string(newEntry.Body) != "new-value" {
		t.Errorf("new entry body: got %q, want %q", newEntry.Body, "new-value")
	}
}

// TestSet_Eviction_FallbackToLiveWhenNoExpired verifies that when the cache is
// full with all-live entries, exactly one is evicted (the existing fallback
// behavior) and the new entry is added.
func TestSet_Eviction_FallbackToLiveWhenNoExpired(t *testing.T) {
	m := NewMemory(3)

	m.Set("a", freshEntry("a", time.Minute))
	m.Set("b", freshEntry("b", time.Minute))
	m.Set("c", freshEntry("c", time.Minute))
	// Cache is now full (3/3), all live.

	m.Set("d", freshEntry("d", time.Minute))

	m.mu.RLock()
	count := len(m.entries)
	_, hasD := m.entries["d"]
	m.mu.RUnlock()

	if count != 3 {
		t.Errorf("entry count: got %d, want 3 (one evicted, one added)", count)
	}
	if !hasD {
		t.Error("new entry d must be present after Set")
	}
}

// TestSet_Eviction_ExpiredSweepMakesRoomNoLiveEviction verifies that when
// sweeping all expired entries alone brings len(m.entries) below maxKeys, no
// live entry is evicted at all.
func TestSet_Eviction_ExpiredSweepMakesRoomNoLiveEviction(t *testing.T) {
	m := NewMemory(4)

	m.Set("exp1", expiredEntry("stale-1"))
	m.Set("exp2", expiredEntry("stale-2"))
	m.Set("live1", freshEntry("l1", time.Minute))
	m.Set("live2", freshEntry("l2", time.Minute))
	// Cache is now full (4/4): 2 expired + 2 live.

	m.Set("new", freshEntry("new-value", time.Minute))

	m.mu.RLock()
	_, hasExp1 := m.entries["exp1"]
	_, hasExp2 := m.entries["exp2"]
	_, hasLive1 := m.entries["live1"]
	_, hasLive2 := m.entries["live2"]
	_, hasNew := m.entries["new"]
	count := len(m.entries)
	m.mu.RUnlock()

	if hasExp1 || hasExp2 {
		t.Error("all expired entries must have been swept")
	}
	if !hasLive1 || !hasLive2 {
		t.Error("both live entries must survive — expired sweep made enough room")
	}
	if !hasNew {
		t.Error("new entry must be present")
	}
	if count != 3 {
		t.Errorf("entry count: got %d, want 3 (2 live + 1 new)", count)
	}
}
