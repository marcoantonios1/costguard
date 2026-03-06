package cache

import (
	"sync"
	"time"
)

type Memory struct {
	mu      sync.RWMutex
	entries map[string]Entry
	maxKeys int
}

func NewMemory(maxKeys int) *Memory {
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	return &Memory{
		entries: make(map[string]Entry),
		maxKeys: maxKeys,
	}
}

func (m *Memory) Get(key string) (Entry, bool) {
	now := time.Now()

	m.mu.RLock()
	entry, ok := m.entries[key]
	m.mu.RUnlock()

	if !ok {
		return Entry{}, false
	}

	if now.After(entry.ExpiresAt) {
		m.mu.Lock()
		delete(m.entries, key)
		m.mu.Unlock()
		return Entry{}, false
	}

	return cloneEntry(entry), true
}

func (m *Memory) Set(key string, entry Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// very simple eviction for Phase A:
	// if full, delete one expired entry first, otherwise delete one arbitrary key
	if len(m.entries) >= m.maxKeys {
		now := time.Now()
		for k, v := range m.entries {
			if now.After(v.ExpiresAt) {
				delete(m.entries, k)
				break
			}
		}
		if len(m.entries) >= m.maxKeys {
			for k := range m.entries {
				delete(m.entries, k)
				break
			}
		}
	}

	m.entries[key] = cloneEntry(entry)
}

func cloneEntry(in Entry) Entry {
	out := Entry{
		StatusCode: in.StatusCode,
		Body:       append([]byte(nil), in.Body...),
		ExpiresAt:  in.ExpiresAt,
	}

	if in.Header != nil {
		out.Header = make(map[string][]string, len(in.Header))
		for k, vv := range in.Header {
			out.Header[k] = append([]string(nil), vv...)
		}
	}

	return out
}
