// Package integration_test multimodal tests exercise image input transforms
// and vision token metering end-to-end across all provider adapters using
// httptest stubs — no real API keys required.
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/budget"
	"github.com/marcoantonios1/costguard/internal/cache"
	"github.com/marcoantonios1/costguard/internal/gateway"
	"github.com/marcoantonios1/costguard/internal/providers"
	anthropic_provider "github.com/marcoantonios1/costguard/internal/providers/anthropic"
	gemini_provider "github.com/marcoantonios1/costguard/internal/providers/gemini"
	openai_provider "github.com/marcoantonios1/costguard/internal/providers/openai"
	openaicompat_provider "github.com/marcoantonios1/costguard/internal/providers/openaicompat"
	openai_http "github.com/marcoantonios1/costguard/internal/server/openai"
	"github.com/marcoantonios1/costguard/internal/usage"
)

// ---------------------------------------------------------------------------
// Vision metering helpers
// ---------------------------------------------------------------------------

// accumulatingStore is an in-memory usage.Store that sums EstimatedCostUSD
// across all saved records so budget.Service can query realistic spend.
type accumulatingStore struct {
	mu      sync.Mutex
	records []usage.Record
}

func (s *accumulatingStore) Save(_ context.Context, r usage.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r)
	return nil
}

func (s *accumulatingStore) totalCost() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sum float64
	for _, r := range s.records {
		sum += r.EstimatedCostUSD
	}
	return sum
}

func (s *accumulatingStore) all() []usage.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]usage.Record, len(s.records))
	copy(cp, s.records)
	return cp
}

func (s *accumulatingStore) GetTotalSpend(_ context.Context, _, _ time.Time) (float64, error) {
	return s.totalCost(), nil
}
func (s *accumulatingStore) GetSpendByTeam(_ context.Context, _, _ time.Time) ([]usage.TeamSpend, error) {
	return nil, nil
}
func (s *accumulatingStore) GetSpendByProvider(_ context.Context, _, _ time.Time) ([]usage.ProviderSpend, error) {
	return nil, nil
}
func (s *accumulatingStore) GetSpendByModel(_ context.Context, _, _ time.Time) ([]usage.ModelSpend, error) {
	return nil, nil
}
func (s *accumulatingStore) GetSpendByProject(_ context.Context, _, _ time.Time) ([]usage.ProjectSpend, error) {
	return nil, nil
}
func (s *accumulatingStore) GetSpendForTeam(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *accumulatingStore) GetSpendForProject(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *accumulatingStore) GetSpendForAgent(_ context.Context, _ string, _, _ time.Time) (float64, error) {
	return 0, nil
}
func (s *accumulatingStore) GetSpendByAgent(_ context.Context, _, _ time.Time) ([]usage.AgentSpend, error) {
	return nil, nil
}

// newHarnessWithBudget wires the gateway with an accumulatingStore and a
// budget.Service configured with the given monthly limit (USD).
func newHarnessWithBudget(reg *providers.Registry, monthlyUSD float64) (*httptest.Server, *accumulatingStore) {
	store := &accumulatingStore{}
	budgetSvc := budget.NewService(store, budget.Config{
		Enabled:    true,
		MonthlyUSD: monthlyUSD,
	})

	gw, err := gateway.New(gateway.Deps{
		Router:        &staticRouter{},
		Registry:      reg,
		Cache:         cache.NewMemory(0), // no caching for budget tests
		CacheTTL:      0,
		UsageStore:    store,
		BudgetChecker: budgetSvc,
	})
	if err != nil {
		panic("gateway.New: " + err.Error())
	}

	mux := http.NewServeMux()
	openai_http.Register(mux, openai_http.Deps{Gateway: gw})
	return httptest.NewServer(mux), store
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// captureUpstream starts a fake upstream at path that records the request body
// sent by the gateway and responds with resp (JSON-encoded, 200 OK).
func captureUpstream(t *testing.T, path string, resp map[string]any) (*httptest.Server, *[]byte) {
	t.Helper()
	var got []byte
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = b
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(mux), &got
}

// rawPost sends a POST to the costguard proxy and returns (statusCode, body)
// without asserting on the status code.
func (h *harness) rawPost(t *testing.T, providerName string, body any) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gateway.HeaderProviderHint, providerName)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func imageOnlyPayload(model, imageURL string) map[string]any {
	return map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": imageURL},
					},
				},
			},
		},
	}
}

func textPlusImagePayload(model, text, imageURL string) map[string]any {
	return map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": text},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": imageURL},
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Anthropic tests
// ---------------------------------------------------------------------------

// TestMultimodal_Anthropic_DataURI: data: URI → upstream receives a base64 image block.
func TestMultimodal_Anthropic_DataURI(t *testing.T) {
	upstream, got := captureUpstream(t, "/v1/messages", anthropicTextResponse())
	defer upstream.Close()

	client, _ := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name: "fake-anthropic-datauri", BaseURL: upstream.URL,
		APIKey: "test-key", AnthropicVersion: "2023-06-01",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-anthropic-datauri", client)
	h := newHarness(reg, &fakeStore{})
	defer h.server.Close()

	status, _ := h.rawPost(t, "fake-anthropic-datauri",
		imageOnlyPayload("claude-3-5-sonnet-20241022", "data:image/png;base64,abc123"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var body struct {
		Messages []struct {
			Content []struct {
				Type   string `json:"type"`
				Source *struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*got, &body); err != nil {
		t.Fatalf("upstream body not valid JSON: %v\nbody: %s", err, *got)
	}
	if len(body.Messages) == 0 || len(body.Messages[0].Content) == 0 {
		t.Fatal("upstream body has no content blocks")
	}
	block := body.Messages[0].Content[0]
	if block.Type != "image" {
		t.Errorf("block.type: got %q, want image", block.Type)
	}
	if block.Source == nil {
		t.Fatal("block.source is nil")
	}
	if block.Source.Type != "base64" {
		t.Errorf("source.type: got %q, want base64", block.Source.Type)
	}
	if block.Source.MediaType != "image/png" {
		t.Errorf("source.media_type: got %q, want image/png", block.Source.MediaType)
	}
	if block.Source.Data != "abc123" {
		t.Errorf("source.data: got %q, want abc123", block.Source.Data)
	}
}

// TestMultimodal_Anthropic_PlainURL: HTTPS URL → upstream receives a url source block.
func TestMultimodal_Anthropic_PlainURL(t *testing.T) {
	upstream, got := captureUpstream(t, "/v1/messages", anthropicTextResponse())
	defer upstream.Close()

	client, _ := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name: "fake-anthropic-url", BaseURL: upstream.URL,
		APIKey: "test-key", AnthropicVersion: "2023-06-01",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-anthropic-url", client)
	h := newHarness(reg, &fakeStore{})
	defer h.server.Close()

	const imageURL = "https://example.com/photo.png"
	status, _ := h.rawPost(t, "fake-anthropic-url",
		imageOnlyPayload("claude-3-5-sonnet-20241022", imageURL))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var body struct {
		Messages []struct {
			Content []struct {
				Type   string `json:"type"`
				Source *struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*got, &body); err != nil {
		t.Fatalf("upstream body not valid JSON: %v\nbody: %s", err, *got)
	}
	block := body.Messages[0].Content[0]
	if block.Type != "image" {
		t.Errorf("block.type: got %q, want image", block.Type)
	}
	if block.Source == nil {
		t.Fatal("block.source is nil")
	}
	if block.Source.Type != "url" {
		t.Errorf("source.type: got %q, want url", block.Source.Type)
	}
	if block.Source.URL != imageURL {
		t.Errorf("source.url: got %q, want %q", block.Source.URL, imageURL)
	}
}

// TestMultimodal_Anthropic_TextAndImage: text + image → correct block order in upstream body.
func TestMultimodal_Anthropic_TextAndImage(t *testing.T) {
	upstream, got := captureUpstream(t, "/v1/messages", anthropicTextResponse())
	defer upstream.Close()

	client, _ := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name: "fake-anthropic-txt", BaseURL: upstream.URL,
		APIKey: "test-key", AnthropicVersion: "2023-06-01",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-anthropic-txt", client)
	h := newHarness(reg, &fakeStore{})
	defer h.server.Close()

	status, _ := h.rawPost(t, "fake-anthropic-txt",
		textPlusImagePayload("claude-3-5-sonnet-20241022", "Describe this image.", "https://example.com/cat.jpg"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var body struct {
		Messages []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*got, &body); err != nil {
		t.Fatalf("upstream body not valid JSON: %v\nbody: %s", err, *got)
	}
	blocks := body.Messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "text" {
		t.Errorf("blocks[0].type: got %q, want text", blocks[0].Type)
	}
	if blocks[0].Text != "Describe this image." {
		t.Errorf("blocks[0].text: got %q", blocks[0].Text)
	}
	if blocks[1].Type != "image" {
		t.Errorf("blocks[1].type: got %q, want image", blocks[1].Type)
	}
}

// ---------------------------------------------------------------------------
// Gemini tests
// ---------------------------------------------------------------------------

// TestMultimodal_Gemini_InlineData: data: URI → upstream receives an inlineData part.
func TestMultimodal_Gemini_InlineData(t *testing.T) {
	upstream, got := captureUpstream(t, "/v1beta/models/", geminiTextResponse())
	defer upstream.Close()

	client, _ := gemini_provider.NewClient(gemini_provider.ClientConfig{
		Name: "fake-gemini-mm", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-gemini-mm", client)
	h := newHarness(reg, &fakeStore{})
	defer h.server.Close()

	status, _ := h.rawPost(t, "fake-gemini-mm",
		imageOnlyPayload("gemini-2.5-flash", "data:image/png;base64,abc123"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var body struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(*got, &body); err != nil {
		t.Fatalf("upstream body not valid JSON: %v\nbody: %s", err, *got)
	}
	if len(body.Contents) == 0 || len(body.Contents[0].Parts) == 0 {
		t.Fatal("upstream body has no parts")
	}
	part := body.Contents[0].Parts[0]
	if part.InlineData == nil {
		t.Fatal("inlineData is nil — expected an inlineData part")
	}
	if part.InlineData.MimeType != "image/png" {
		t.Errorf("inlineData.mimeType: got %q, want image/png", part.InlineData.MimeType)
	}
	if part.InlineData.Data != "abc123" {
		t.Errorf("inlineData.data: got %q, want abc123", part.InlineData.Data)
	}
}

// ---------------------------------------------------------------------------
// OpenAI tests
// ---------------------------------------------------------------------------

// TestMultimodal_OpenAI_Passthrough: image content reaches upstream with image_url block intact.
func TestMultimodal_OpenAI_Passthrough(t *testing.T) {
	upstream, got := captureUpstream(t, "/v1/chat/completions", openAITextResponse())
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fake-openai-mm", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-openai-mm", client)
	h := newHarness(reg, &fakeStore{})
	defer h.server.Close()

	const imageURL = "https://example.com/photo.png"
	status, _ := h.rawPost(t, "fake-openai-mm",
		imageOnlyPayload("gpt-4o-mini", imageURL))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var body struct {
		Messages []struct {
			Content []struct {
				Type     string `json:"type"`
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*got, &body); err != nil {
		t.Fatalf("upstream body not valid JSON: %v\nbody: %s", err, *got)
	}
	if len(body.Messages) == 0 || len(body.Messages[0].Content) == 0 {
		t.Fatal("upstream body has no content blocks")
	}
	block := body.Messages[0].Content[0]
	if block.Type != "image_url" {
		t.Errorf("block.type: got %q, want image_url", block.Type)
	}
	if block.ImageURL == nil || block.ImageURL.URL != imageURL {
		t.Errorf("image_url.url: got %v, want %q", block.ImageURL, imageURL)
	}
}

// ---------------------------------------------------------------------------
// OpenAI-compatible guard tests
// ---------------------------------------------------------------------------

// TestMultimodal_CompatGuard_Rejects: image with allow_multimodal:false → 400, upstream not called.
func TestMultimodal_CompatGuard_Rejects(t *testing.T) {
	var upstreamCalled bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	client, _ := openaicompat_provider.NewClient(openaicompat_provider.ClientConfig{
		Name: "fake-compat-guard", BaseURL: upstream.URL, AllowMultimodal: false,
	})
	reg := providers.NewRegistry()
	reg.Register("fake-compat-guard", client)
	h := newHarness(reg, &fakeStore{})
	defer h.server.Close()

	status, body := h.rawPost(t, "fake-compat-guard",
		imageOnlyPayload("llama3", "https://example.com/img.png"))

	if status != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", status)
	}
	if upstreamCalled {
		t.Error("upstream should not have been called when the multimodal guard is active")
	}

	var errBody struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &errBody); err != nil {
		t.Fatalf("error response is not valid JSON: %v\nbody: %s", err, body)
	}
	if errBody.Error.Type != "invalid_request_error" {
		t.Errorf("error.type: got %q, want invalid_request_error", errBody.Error.Type)
	}
	if errBody.Error.Message == "" {
		t.Error("error.message should not be empty")
	}
}

// TestMultimodal_CompatGuard_Allows: image with allow_multimodal:true → forwarded to upstream.
func TestMultimodal_CompatGuard_Allows(t *testing.T) {
	upstream, got := captureUpstream(t, "/v1/chat/completions", openAITextResponse())
	defer upstream.Close()

	client, _ := openaicompat_provider.NewClient(openaicompat_provider.ClientConfig{
		Name: "fake-compat-allow", BaseURL: upstream.URL, AllowMultimodal: true,
	})
	reg := providers.NewRegistry()
	reg.Register("fake-compat-allow", client)
	h := newHarness(reg, &fakeStore{})
	defer h.server.Close()

	const imageURL = "https://example.com/img.png"
	status, _ := h.rawPost(t, "fake-compat-allow",
		imageOnlyPayload("llama3", imageURL))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	if len(*got) == 0 {
		t.Error("upstream received empty body — request was not forwarded")
	}

	var body struct {
		Messages []struct {
			Content []struct {
				Type     string `json:"type"`
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(*got, &body); err != nil {
		t.Fatalf("upstream body not valid JSON: %v\nbody: %s", err, *got)
	}
	block := body.Messages[0].Content[0]
	if block.Type != "image_url" {
		t.Errorf("block.type: got %q, want image_url", block.Type)
	}
	if block.ImageURL == nil || block.ImageURL.URL != imageURL {
		t.Errorf("image_url.url: got %v, want %q", block.ImageURL, imageURL)
	}
}

// ---------------------------------------------------------------------------
// Vision token metering helpers
// ---------------------------------------------------------------------------

func anthropicZeroUsageResponse() map[string]any {
	return map[string]any{
		"id": "msg_zero", "type": "message", "role": "assistant",
		"model":       "claude-sonnet-4-5-20250929",
		"content":     []any{map[string]any{"type": "text", "text": "I see an image."}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 0, "output_tokens": 0},
	}
}

func openAIZeroUsageResponse() map[string]any {
	return map[string]any{
		"id": "chatcmpl-zero", "object": "chat.completion",
		"created": 1000003, "model": "gpt-4o-mini",
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": "I see an image."},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	}
}

func postToServer(t *testing.T, srv *httptest.Server, providerName string, body any) (int, []byte) {
	t.Helper()
	payload, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gateway.HeaderProviderHint, providerName)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

// ---------------------------------------------------------------------------
// Vision token correction tests
// ---------------------------------------------------------------------------

// TestMultimodal_VisionTokenCorrection_Anthropic: upstream reports 0 tokens for a
// vision request — gateway must apply the Anthropic tile formula and record the
// corrected prompt token count in the usage store.
func TestMultimodal_VisionTokenCorrection_Anthropic(t *testing.T) {
	upstream, _ := captureUpstream(t, "/v1/messages", anthropicZeroUsageResponse())
	defer upstream.Close()

	client, _ := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name: "fake-anthropic-vision", BaseURL: upstream.URL,
		APIKey: "test-key", AnthropicVersion: "2023-06-01",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-anthropic-vision", client)
	srv, store := newHarnessWithBudget(reg, 1000)
	defer srv.Close()

	status, _ := postToServer(t, srv, "fake-anthropic-vision",
		imageOnlyPayload("claude-sonnet-4-5-20250929", "https://example.com/photo.png"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	records := store.all()
	if len(records) == 0 {
		t.Fatal("no usage records saved")
	}
	got := records[len(records)-1].PromptTokens
	const want = 3125 // 1024×1024 default → 4 tiles → 4×765+65
	if got != want {
		t.Errorf("PromptTokens: got %d, want %d", got, want)
	}
}

// TestMultimodal_VisionTokenCorrection_OpenAI: upstream reports 0 tokens for a
// vision request — gateway must apply the OpenAI tile formula and record the
// corrected prompt token count in the usage store.
func TestMultimodal_VisionTokenCorrection_OpenAI(t *testing.T) {
	upstream, _ := captureUpstream(t, "/v1/chat/completions", openAIZeroUsageResponse())
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fake-openai-vision", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-openai-vision", client)
	srv, store := newHarnessWithBudget(reg, 1000)
	defer srv.Close()

	status, _ := postToServer(t, srv, "fake-openai-vision",
		imageOnlyPayload("gpt-4o-mini", "https://example.com/photo.png"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	records := store.all()
	if len(records) == 0 {
		t.Fatal("no usage records saved")
	}
	got := records[len(records)-1].PromptTokens
	const want = 765 // 1024×1024 → resize 768×768 → 4 tiles → 4×170+85
	if got != want {
		t.Errorf("PromptTokens: got %d, want %d", got, want)
	}
}

// TestMultimodal_VisionTokenCorrection_NonZeroUnchanged: when the upstream
// already reports a non-zero prompt token count the gateway must not overwrite
// it with a client-side estimate.
func TestMultimodal_VisionTokenCorrection_NonZeroUnchanged(t *testing.T) {
	upstream, _ := captureUpstream(t, "/v1/chat/completions", openAITextResponse())
	defer upstream.Close()

	client, _ := openai_provider.NewClient(openai_provider.ClientConfig{
		Name: "fake-openai-nonzero", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-openai-nonzero", client)
	srv, store := newHarnessWithBudget(reg, 1000)
	defer srv.Close()

	status, _ := postToServer(t, srv, "fake-openai-nonzero",
		imageOnlyPayload("gpt-4o-mini", "https://example.com/photo.png"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	records := store.all()
	if len(records) == 0 {
		t.Fatal("no usage records saved")
	}
	got := records[len(records)-1].PromptTokens
	const want = 150 // upstream reported 150 — correction must not apply
	if got != want {
		t.Errorf("PromptTokens: got %d, want %d (correction must not override non-zero upstream count)", got, want)
	}
}

// TestMultimodal_Gemini_TrustsUpstreamTokens: Gemini reports image tokens in
// usageMetadata; the gateway must not apply a client-side estimate for the
// gemini family even when the upstream returns zero tokens.
func TestMultimodal_Gemini_TrustsUpstreamTokens(t *testing.T) {
	geminiZero := map[string]any{
		"candidates": []any{map[string]any{
			"content": map[string]any{
				"role":  "model",
				"parts": []any{map[string]any{"text": "I see an image."}},
			},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount": 0, "candidatesTokenCount": 0, "totalTokenCount": 0,
		},
		"modelVersion": "gemini-2.5-flash",
	}

	upstream, _ := captureUpstream(t, "/v1beta/models/", geminiZero)
	defer upstream.Close()

	client, _ := gemini_provider.NewClient(gemini_provider.ClientConfig{
		Name: "fake-gemini-vision", BaseURL: upstream.URL, APIKey: "test-key",
	})
	reg := providers.NewRegistry()
	reg.Register("fake-gemini-vision", client)
	srv, store := newHarnessWithBudget(reg, 1000)
	defer srv.Close()

	status, _ := postToServer(t, srv, "fake-gemini-vision",
		imageOnlyPayload("gemini-2.5-flash", "data:image/png;base64,abc123"))
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	records := store.all()
	if len(records) == 0 {
		t.Fatal("no usage records saved")
	}
	got := records[len(records)-1].PromptTokens
	if got != 0 {
		t.Errorf("PromptTokens: got %d, want 0 — Gemini tokens must not be client-estimated", got)
	}
}

// TestMultimodal_BudgetBlocks_AfterVisionMetering: after a vision request is
// metered (with the client-side token estimate adding cost), a subsequent
// request must be blocked with 402 when the recorded spend exceeds the budget.
func TestMultimodal_BudgetBlocks_AfterVisionMetering(t *testing.T) {
	upstream, _ := captureUpstream(t, "/v1/messages", anthropicZeroUsageResponse())
	defer upstream.Close()

	// Provider name starts with "anthropic" so NormalizeProvider → "anthropic",
	// enabling a real price lookup and non-zero EstimatedCostUSD in the store.
	client, _ := anthropic_provider.NewClient(anthropic_provider.ClientConfig{
		Name: "anthropic-budget", BaseURL: upstream.URL,
		APIKey: "test-key", AnthropicVersion: "2023-06-01",
	})
	reg := providers.NewRegistry()
	reg.Register("anthropic-budget", client)

	// Budget smaller than the cost of one vision request:
	// 3125 tokens × ($0.75/1M) ≈ $0.0000023 > $0.000001
	srv, store := newHarnessWithBudget(reg, 0.000001)
	defer srv.Close()

	// First request: budget not yet exceeded — must succeed.
	status1, _ := postToServer(t, srv, "anthropic-budget",
		imageOnlyPayload("claude-sonnet-4-5-20250929", "https://example.com/photo.png"))
	if status1 != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", status1)
	}

	if store.totalCost() <= 0 {
		t.Fatal("expected non-zero cost after first vision request")
	}

	// Second request: spend now exceeds budget → must return 402.
	status2, body2 := postToServer(t, srv, "anthropic-budget",
		imageOnlyPayload("claude-sonnet-4-5-20250929", "https://example.com/photo.png"))
	if status2 != http.StatusPaymentRequired {
		t.Errorf("second request: expected 402 after budget exceeded, got %d\nbody: %s", status2, body2)
	}
}
