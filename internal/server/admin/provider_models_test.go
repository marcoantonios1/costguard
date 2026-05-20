package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/server/admin"
)

// stubCatalogGetter satisfies ProviderCatalogGetter via a fixed map.
type stubCatalogGetter struct {
	items map[string]providers.RuntimeMetadata
}

func (s *stubCatalogGetter) Get(name string) (providers.RuntimeMetadata, bool) {
	md, ok := s.items[name]
	return md, ok
}

func TestProviderModelsHandler_KnownProvider_WithModels(t *testing.T) {
	catalog := &stubCatalogGetter{items: map[string]providers.RuntimeMetadata{
		"anthropic_primary": {
			Name:            "anthropic_primary",
			Enabled:         true,
			SupportedModels: []string{"claude-sonnet-4-6", "claude-opus-4-6"},
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/providers/anthropic_primary/models", nil)
	req.SetPathValue("name", "anthropic_primary")
	w := httptest.NewRecorder()
	admin.ProviderModelsHandler(catalog)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Models) != 2 {
		t.Fatalf("expected 2 models, got %d: %v", len(resp.Models), resp.Models)
	}
	if resp.Models[0] != "claude-sonnet-4-6" || resp.Models[1] != "claude-opus-4-6" {
		t.Errorf("unexpected models: %v", resp.Models)
	}
}

func TestProviderModelsHandler_KnownProvider_Unconstrained(t *testing.T) {
	catalog := &stubCatalogGetter{items: map[string]providers.RuntimeMetadata{
		"openai_primary": {Name: "openai_primary", Enabled: true},
	}}

	req := httptest.NewRequest(http.MethodGet, "/providers/openai_primary/models", nil)
	req.SetPathValue("name", "openai_primary")
	w := httptest.NewRecorder()
	admin.ProviderModelsHandler(catalog)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Models []string `json:"models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Models) != 0 {
		t.Errorf("expected empty models for unconstrained provider, got %v", resp.Models)
	}
}

func TestProviderModelsHandler_UnknownProvider_404(t *testing.T) {
	catalog := &stubCatalogGetter{items: map[string]providers.RuntimeMetadata{}}

	req := httptest.NewRequest(http.MethodGet, "/providers/nonexistent/models", nil)
	req.SetPathValue("name", "nonexistent")
	w := httptest.NewRecorder()
	admin.ProviderModelsHandler(catalog)(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}
