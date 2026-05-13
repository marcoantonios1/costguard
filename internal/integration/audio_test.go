package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

func newAudioHarness(baseURL string, store *fakeStore) (*httptest.Server, error) {
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

// postAudio sends a multipart POST to /v1/audio/transcriptions.
func postAudio(t *testing.T, srv *httptest.Server, providerHint string) (int, []byte) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", "whisper-1")
	fw, _ := mw.CreateFormFile("file", "audio.mp3")
	_, _ = fw.Write([]byte("fake audio bytes"))
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/audio/transcriptions", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if providerHint != "" {
		req.Header.Set(gateway.HeaderProviderHint, providerHint)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /v1/audio/transcriptions: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestAudioTranscriptions_ProxiesToUpstream verifies that a multipart request
// is forwarded to the upstream and the {"text":"..."} response is returned.
func TestAudioTranscriptions_ProxiesToUpstream(t *testing.T) {
	upstream, upstreamCalls := alwaysSucceedUpstream(t, "/v1/audio/transcriptions", map[string]any{"text": "hello world"})
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newAudioHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, body := postAudio(t, srv, "openai_primary")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("response is not JSON: %v — body: %s", err, body)
	}
	if got["text"] != "hello world" {
		t.Errorf(`expected text="hello world", got %s`, body)
	}

	if n := upstreamCalls.Load(); n != 1 {
		t.Errorf("upstream calls: got %d, want 1", n)
	}
}

// TestAudioTranscriptions_UsageRecorded verifies that a successful request
// produces a usage record with MeteringEstimated=true and the correct fields.
func TestAudioTranscriptions_UsageRecorded(t *testing.T) {
	upstream, _ := alwaysSucceedUpstream(t, "/v1/audio/transcriptions", map[string]any{"text": "hello"})
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newAudioHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, body := postAudio(t, srv, "openai_primary")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}

	records := store.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(records))
	}
	rec := records[0]
	if rec.Provider != "openai_primary" {
		t.Errorf("record.Provider: got %q, want openai_primary", rec.Provider)
	}
	if rec.Model != "whisper-1" {
		t.Errorf("record.Model: got %q, want whisper-1", rec.Model)
	}
	if !rec.MeteringEstimated {
		t.Error("record.MeteringEstimated: want true (Whisper responses carry no token counts)")
	}
	if rec.Path != "/v1/audio/transcriptions" {
		t.Errorf("record.Path: got %q, want /v1/audio/transcriptions", rec.Path)
	}
}

// TestAudioTranscriptions_UnknownProvider_Returns400 verifies that an
// unrecognised X-Costguard-Provider hint yields 400.
func TestAudioTranscriptions_UnknownProvider_Returns400(t *testing.T) {
	upstream, _ := alwaysSucceedUpstream(t, "/v1/audio/transcriptions", map[string]any{"text": "x"})
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newAudioHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	status, _ := postAudio(t, srv, "nonexistent-provider")
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown provider, got %d", status)
	}
}

// TestAudioTranscriptions_MethodNotAllowed verifies that GET returns 405.
func TestAudioTranscriptions_MethodNotAllowed(t *testing.T) {
	upstream, _ := alwaysSucceedUpstream(t, "/v1/audio/transcriptions", map[string]any{"text": "x"})
	defer upstream.Close()

	store := &fakeStore{}
	srv, err := newAudioHarness(upstream.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/audio/transcriptions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}
