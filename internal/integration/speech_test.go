package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
)

// fakeAudioBytes is a stand-in for real audio output in tests.
var fakeAudioBytes = []byte("RIFF....fake-mp3-payload")

// ttsUpstream starts a fake TTS upstream that returns raw audio bytes with
// Content-Type: audio/mpeg, counting each call.
func ttsUpstream(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fakeAudioBytes)
	})
	return httptest.NewServer(mux), &calls
}

// newSpeechHarness wires a gateway with a single OpenAI-compatible provider.
func newSpeechHarness(baseURL string, store *fakeStore) (*httptest.Server, error) {
	client, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name:    "openai_primary",
		BaseURL: baseURL,
		APIKey:  "test-key",
	})
	if err != nil {
		return nil, err
	}

	reg := providers.NewRegistry()
	reg.Register("openai_primary", client)

	gw, err := gateway.New(gateway.Deps{
		Router:     &staticRouter{},
		Registry:   reg,
		Cache:      cache.NewMemory(0),
		UsageStore: store,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux), nil
}

// postSpeech sends a POST /v1/audio/speech with a JSON TTS body.
func postSpeech(t *testing.T, srv *httptest.Server, providerHint string, input string) (int, http.Header, []byte) {
	t.Helper()

	payload, _ := json.Marshal(map[string]any{
		"model": "tts-1",
		"input": input,
		"voice": "alloy",
	})

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/audio/speech", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if providerHint != "" {
		req.Header.Set(gateway.HeaderProviderHint, providerHint)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/audio/speech: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestSpeech_ProxiesToUpstream verifies the audio bytes are forwarded verbatim
// and the Content-Type header from the upstream is preserved.
func TestSpeech_ProxiesToUpstream(t *testing.T) {
	upstream, calls := ttsUpstream(t)
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newSpeechHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, headers, body := postSpeech(t, srv, "openai_primary", "Hello world")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	if ct := headers.Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type: got %q, want audio/mpeg", ct)
	}
	if !bytes.Equal(body, fakeAudioBytes) {
		t.Errorf("body mismatch: got %q, want %q", body, fakeAudioBytes)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("upstream calls: got %d, want 1", n)
	}
}

// TestSpeech_UsageRecorded verifies that a successful request produces a
// usage record with the correct character count in PromptTokens.
func TestSpeech_UsageRecorded(t *testing.T) {
	upstream, _ := ttsUpstream(t)
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newSpeechHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	const input = "Hello world"
	status, _, _ := postSpeech(t, srv, "openai_primary", input)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	rec := records[0]
	if rec.Provider != "openai_primary" {
		t.Errorf("record.Provider: got %q, want openai_primary", rec.Provider)
	}
	if rec.Model != "tts-1" {
		t.Errorf("record.Model: got %q, want tts-1", rec.Model)
	}
	if rec.PromptTokens != len([]rune(input)) {
		t.Errorf("record.PromptTokens: got %d, want %d (char count)", rec.PromptTokens, len([]rune(input)))
	}
	if !rec.MeteringEstimated {
		t.Error("record.MeteringEstimated: want true (TTS billed per character)")
	}
	if rec.Path != "/v1/audio/speech" {
		t.Errorf("record.Path: got %q, want /v1/audio/speech", rec.Path)
	}
}

// TestSpeech_UnknownProvider_Returns400 verifies that an unrecognised
// X-Costguard-Provider hint yields 400.
func TestSpeech_UnknownProvider_Returns400(t *testing.T) {
	upstream, _ := ttsUpstream(t)
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newSpeechHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, _, _ := postSpeech(t, srv, "nonexistent-provider", "Hello")
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown provider, got %d", status)
	}
}

// TestSpeech_MethodNotAllowed verifies that GET returns 405.
func TestSpeech_MethodNotAllowed(t *testing.T) {
	upstream, _ := ttsUpstream(t)
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newSpeechHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/audio/speech", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}
