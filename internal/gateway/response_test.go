package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/providers"
)

func TestNewJSONErrorResponse_ValidJSON(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "quotes and backslash",
			message: `upstream said "bad request" with path C:\foo\bar`,
		},
		{
			name:    "newline in message",
			message: "line one\nline two",
		},
		{
			name:    "hostile header injection",
			message: `foo","injected_key":"bar`,
		},
		{
			name:    "empty string",
			message: "",
		},
		{
			name:    "unicode",
			message: "日本語エラー  ",
		},
		{
			name:    "only special chars",
			message: `"""\\\\nnn`,
		},
		{
			name:    "very long string",
			message: strings.Repeat("a", 10_000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := newJSONErrorResponse(nil, http.StatusBadRequest, tt.message)

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("reading body: %v", err)
			}

			// Must unmarshal into ErrorBody cleanly (round-trip check).
			var eb providers.ErrorBody
			if err := json.Unmarshal(bodyBytes, &eb); err != nil {
				t.Fatalf("body is not valid JSON: %v\nbody: %s", err, bodyBytes)
			}
			if eb.Error.Message != tt.message {
				t.Errorf("message round-trip failed\ngot:  %q\nwant: %q", eb.Error.Message, tt.message)
			}

			// Must also unmarshal into a generic map — and must NOT contain injected keys.
			var raw map[string]any
			if err := json.Unmarshal(bodyBytes, &raw); err != nil {
				t.Fatalf("body not valid JSON (map): %v", err)
			}

			errObj, ok := raw["error"].(map[string]any)
			if !ok {
				t.Fatalf("top-level 'error' key missing or wrong type")
			}

			for key := range errObj {
				if key != "message" && key != "type" && key != "code" && key != "category" {
					t.Errorf("unexpected key %q injected into error object", key)
				}
			}

			// Specifically guard against the header-injection key.
			if _, found := errObj["injected_key"]; found {
				t.Error("injected_key must not appear in the error object")
			}
		})
	}
}

func TestResponseFromCacheEntry_ContentLengthMatchesBody(t *testing.T) {
	body := []byte(`{"id":"x","object":"chat.completion","choices":[]}`)

	entry := cache.Entry{
		StatusCode: http.StatusOK,
		Header: map[string][]string{
			"Content-Type": {"application/json"},
			// Intentionally wrong length to confirm it gets recomputed.
			"Content-Length": {"9999"},
		},
		Body:      body,
		ExpiresAt: time.Now().Add(time.Minute),
	}

	resp := responseFromCacheEntry(nil, entry)
	defer resp.Body.Close()

	got := resp.Header.Get("Content-Length")
	want := strconv.Itoa(len(body))
	if got != want {
		t.Errorf("Content-Length: got %q, want %q", got, want)
	}

	if resp.Header.Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding must be absent, got %q", resp.Header.Get("Content-Encoding"))
	}

	replayed, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(replayed) != string(body) {
		t.Errorf("body mismatch\ngot:  %q\nwant: %q", replayed, body)
	}
}
