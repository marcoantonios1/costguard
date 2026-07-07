package feedback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store persists feedback records.
type Store interface {
	Append(ctx context.Context, r FeedbackRecord) error
}

// JSONLStore appends newline-delimited JSON records to a file. It is safe
// for concurrent use.
type JSONLStore struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

// NewJSONLStore opens (creating if necessary) the file at path for appending.
func NewJSONLStore(path string) (*JSONLStore, error) {
	if path == "" {
		return nil, errors.New("feedback: log path must not be empty")
	}

	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("feedback: mkdir %s: %w", dir, err)
		}
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("feedback: open %s: %w", path, err)
	}

	return &JSONLStore{path: path, f: f}, nil
}

// Append writes r as a single JSON line to the file.
func (s *JSONLStore) Append(ctx context.Context, r FeedbackRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = fmt.Fprintf(s.f, "%s\n", b)
	return err
}
