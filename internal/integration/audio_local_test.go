package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
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

// ---------------------------------------------------------------------------
// Local-provider harnesses
// ---------------------------------------------------------------------------

// newLocalAudioHarness wires a gateway with AudioTranscriptionProvider=local
// pointing at localURL. No OpenAI provider is registered.
func newLocalAudioHarness(localURL string, store *fakeStore) (*httptest.Server, error) {
	reg := providers.NewRegistry()

	gw, err := gateway.New(gateway.Deps{
		Router:                     &staticRouter{},
		Registry:                   reg,
		Cache:                      cache.NewMemory(0),
		UsageStore:                 store,
		AudioTranscriptionProvider: "local",
		AudioTranscriptionURL:      localURL,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux), nil
}

// newLocalTTSHarness wires a gateway with AudioTTSProvider=local pointing at
// localURL. No OpenAI provider is registered.
func newLocalTTSHarness(localURL string, store *fakeStore) (*httptest.Server, error) {
	reg := providers.NewRegistry()

	gw, err := gateway.New(gateway.Deps{
		Router:           &staticRouter{},
		Registry:         reg,
		Cache:            cache.NewMemory(0),
		UsageStore:       store,
		AudioTTSProvider: "local",
		AudioTTSURL:      localURL,
	})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux), nil
}

// localTranscriptionUpstream starts a fake STT server that records whether
// the multipart body was received intact and returns a JSON transcription.
func localTranscriptionUpstream(t *testing.T) (*httptest.Server, *atomic.Int32, *[]byte) {
	t.Helper()
	var calls atomic.Int32
	var receivedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		receivedBody = b
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "local transcription"})
	})
	return httptest.NewServer(mux), &calls, &receivedBody
}

// localTTSUpstream starts a fake TTS server that records whether the JSON
// body was received intact and returns raw audio bytes.
func localTTSUpstream(t *testing.T) (*httptest.Server, *atomic.Int32, *[]byte) {
	t.Helper()
	var calls atomic.Int32
	var receivedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		receivedBody = b
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fakeAudioBytes)
	})
	return httptest.NewServer(mux), &calls, &receivedBody
}

// ---------------------------------------------------------------------------
// Test 1 — Local transcription routing
// ---------------------------------------------------------------------------

// TestLocalAudio_RoutesToLocalURL verifies that when AudioTranscriptionProvider
// is "local" the request is forwarded to the configured local URL, the
// multipart body is preserved, and the response is returned unchanged.
func TestLocalAudio_RoutesToLocalURL(t *testing.T) {
	upstream, calls, receivedBody := localTranscriptionUpstream(t)
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newLocalAudioHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// Build multipart body.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", "whisper-1")
	fw, _ := mw.CreateFormFile("file", "audio.mp3")
	_, _ = fw.Write([]byte("fake audio bytes"))
	_ = mw.Close()
	sentBody := buf.Bytes()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/audio/transcriptions", bytes.NewReader(sentBody))
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/audio/transcriptions: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	// Local upstream was called exactly once.
	if n := calls.Load(); n != 1 {
		t.Errorf("local upstream calls: got %d, want 1", n)
	}

	// Multipart body was preserved.
	if !bytes.Equal(*receivedBody, sentBody) {
		t.Errorf("request body not preserved: got %d bytes, sent %d bytes", len(*receivedBody), len(sentBody))
	}

	// Response was returned unchanged.
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("response is not JSON: %v — body: %s", err, body)
	}
	if got["text"] != "local transcription" {
		t.Errorf(`expected text="local transcription", got %s`, body)
	}
}

// ---------------------------------------------------------------------------
// Test 2 — Local TTS routing
// ---------------------------------------------------------------------------

// TestLocalTTS_RoutesToLocalURL verifies that when AudioTTSProvider is "local"
// the request is forwarded to the configured local URL, the JSON body is
// preserved, and the audio response is streamed back unchanged.
func TestLocalTTS_RoutesToLocalURL(t *testing.T) {
	upstream, calls, receivedBody := localTTSUpstream(t)
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newLocalTTSHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	payload, _ := json.Marshal(map[string]any{
		"model": "tts-1",
		"input": "Hello local",
		"voice": "alloy",
	})

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/audio/speech", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/audio/speech: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Local upstream was called exactly once.
	if n := calls.Load(); n != 1 {
		t.Errorf("local TTS upstream calls: got %d, want 1", n)
	}

	// JSON body was preserved.
	if !bytes.Equal(*receivedBody, payload) {
		t.Errorf("request body not preserved: got %q, want %q", *receivedBody, payload)
	}

	// Audio response was returned unchanged.
	if !bytes.Equal(body, fakeAudioBytes) {
		t.Errorf("audio body mismatch: got %q, want %q", body, fakeAudioBytes)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type: got %q, want audio/mpeg", ct)
	}
}

// ---------------------------------------------------------------------------
// Test 3 — OpenAI provider behavior unchanged
// ---------------------------------------------------------------------------

// TestOpenAIAudio_BehaviorUnchanged guarantees that when AudioTranscriptionProvider
// is "openai" (the default) routing still goes through the registered provider
// registry and no local URLs are involved.
func TestOpenAIAudio_BehaviorUnchanged(t *testing.T) {
	upstream, upstreamCalls := alwaysSucceedUpstream(t, "/v1/audio/transcriptions", map[string]any{"text": "openai result"})
	defer upstream.Close()

	// Wire a gateway explicitly with AudioTranscriptionProvider="openai".
	client, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name:    "openai_primary",
		BaseURL: upstream.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := providers.NewRegistry()
	reg.Register("openai_primary", client)

	store := &fakeStore{}
	gw, err := gateway.New(gateway.Deps{
		Router:                     &staticRouter{},
		Registry:                   reg,
		Cache:                      cache.NewMemory(0),
		UsageStore:                 store,
		AudioTranscriptionProvider: "openai",
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	status, body := postAudio(t, srv, "openai_primary")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if got["text"] != "openai result" {
		t.Errorf(`expected text="openai result", got %s`, body)
	}

	// The registered OpenAI provider was called, not any local URL.
	if n := upstreamCalls.Load(); n != 1 {
		t.Errorf("openai upstream calls: got %d, want 1", n)
	}
}

// TestOpenAITTS_BehaviorUnchanged guarantees that when AudioTTSProvider is
// "openai" routing still goes through the registered provider registry.
func TestOpenAITTS_BehaviorUnchanged(t *testing.T) {
	upstream, calls := ttsUpstream(t)
	defer upstream.Close()

	client, err := openai_provider.NewClient(openai_provider.ClientConfig{
		Name:    "openai_primary",
		BaseURL: upstream.URL,
		APIKey:  "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg := providers.NewRegistry()
	reg.Register("openai_primary", client)

	store := &fakeStore{}
	gw, err := gateway.New(gateway.Deps{
		Router:           &staticRouter{},
		Registry:         reg,
		Cache:            cache.NewMemory(0),
		UsageStore:       store,
		AudioTTSProvider: "openai",
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	status, _, body := postSpeech(t, srv, "openai_primary", "Hello openai")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if !bytes.Equal(body, fakeAudioBytes) {
		t.Errorf("audio body mismatch")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("openai TTS upstream calls: got %d, want 1", n)
	}
}
