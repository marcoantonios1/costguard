package gateway

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/cache"
)

func newJSONErrorResponse(r *http.Request, status int, message string) *http.Response {
	body := fmt.Sprintf(`{"error":{"message":"%s"}}`, message)

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		Request:    r,
	}
}

func cloneHeader(h http.Header) map[string][]string {
	out := make(map[string][]string)

	for k, vv := range h {
		switch http.CanonicalHeaderKey(k) {
		case "Content-Type",
			"Content-Length",
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
