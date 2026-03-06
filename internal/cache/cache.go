package cache

import "time"

type Entry struct {
	StatusCode int
	Header     map[string][]string
	Body       []byte
	ExpiresAt  time.Time
}

type Cache interface {
	Get(key string) (Entry, bool)
	Set(key string, entry Entry)
}
