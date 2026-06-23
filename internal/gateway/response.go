package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/providers"
)

// newJSONErrorResponseCategorized builds a JSON error response that includes
// the taxonomy category and normalized type string in the body.
func newJSONErrorResponseCategorized(r *http.Request, status int, message, errType, category string) *http.Response {
	var out providers.ErrorBody
	out.Error.Message = message
	out.Error.Type = errType
	out.Error.Category = category

	body, err := json.Marshal(out)
	if err != nil {
		body = []byte(`{"error":{"message":"internal error"}}`)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    r,
	}
}

func newJSONErrorResponse(r *http.Request, status int, message string) *http.Response {
	var out providers.ErrorBody
	out.Error.Message = message

	body, err := json.Marshal(out)
	if err != nil {
		body = []byte(`{"error":{"message":"internal error"}}`)
	}

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    r,
	}
}

func cloneHeader(h http.Header) map[string][]string {
	out := make(map[string][]string)

	for k, vv := range h {
		switch http.CanonicalHeaderKey(k) {
		case "Content-Type",
			// Content-Length is intentionally excluded here; responseFromCacheEntry
			// recomputes it from the stored body length so a stale value from the
			// original encoded response can never mismatch the replayed body.
			// Content-Encoding is intentionally excluded; cached bodies are always
			// stored decoded, so replaying a Content-Encoding header would be wrong.
			"Openai-Organization",
			"Openai-Processing-Ms",
			"Openai-Project",
			"Openai-Version":
			out[k] = append([]string(nil), vv...)
		}
	}

	return out
}

func responseFromCacheEntry(r *http.Request, entry cache.Entry) *http.Response {
	header := make(http.Header, len(entry.Header))
	for k, vv := range entry.Header {
		header[k] = append([]string(nil), vv...)
	}
	header.Set("Content-Length", strconv.Itoa(len(entry.Body)))

	return &http.Response{
		StatusCode: entry.StatusCode,
		Status:     fmt.Sprintf("%d %s", entry.StatusCode, http.StatusText(entry.StatusCode)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(entry.Body)),
		Request:    r,
	}
}

func cacheEntryFromResponse(resp *http.Response, body []byte, ttl time.Duration) cache.Entry {
	return cache.Entry{
		StatusCode: resp.StatusCode,
		Header:     cloneHeader(resp.Header),
		Body:       append([]byte(nil), body...),
		ExpiresAt:  time.Now().Add(ttl),
	}
}
