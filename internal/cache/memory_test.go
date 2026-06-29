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
