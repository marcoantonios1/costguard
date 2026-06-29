package gateway

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

// TestCloneRequestWithBody_StaleContentLengthRemoved verifies that a stale
// Content-Length header (written before a body rewrite, e.g. a model-name
// substitution) is removed from the cloned request's header map, and that the
// ContentLength struct field reflects the new body length.
func TestCloneRequestWithBody_StaleContentLengthRemoved(t *testing.T) {
	originalBody := []byte(`{"model":"gpt-4","messages":[]}`)
	req, err := http.NewRequest(http.MethodPost, "http://example.com/v1/chat/completions", bytes.NewReader(originalBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	// Simulate a stale Content-Length set by the inbound client before the body
	// was rewritten (e.g. model name substituted, changing the byte length).
	req.Header.Set("Content-Length", "9999")

	newBody := []byte(`{"model":"llama3","messages":[]}`)
	clone := cloneRequestWithBody(req, newBody)

	if got := clone.Header.Get("Content-Length"); got != "" {
		t.Errorf("Content-Length header: got %q, want empty (stale value must be removed)", got)
	}
	if clone.ContentLength != int64(len(newBody)) {
		t.Errorf("ContentLength field: got %d, want %d", clone.ContentLength, len(newBody))
	}

	// Confirm the body is readable and correct.
	body, _ := io.ReadAll(clone.Body)
	if !bytes.Equal(body, newBody) {
		t.Errorf("body: got %q, want %q", body, newBody)
	}
}

// TestCloneRequestWithBody_NoContentLengthHeader verifies that a request
// without a Content-Length header (the normal case) is also handled correctly.
func TestCloneRequestWithBody_NoContentLengthHeader(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://example.com/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	body := []byte(`{"model":"llama3","messages":[{"role":"user","content":"hi"}]}`)
	clone := cloneRequestWithBody(req, body)

	if got := clone.Header.Get("Content-Length"); got != "" {
		t.Errorf("Content-Length header: got %q, want empty", got)
	}
	if clone.ContentLength != int64(len(body)) {
		t.Errorf("ContentLength field: got %d, want %d", clone.ContentLength, len(body))
	}
}
