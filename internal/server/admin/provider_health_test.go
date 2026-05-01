package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/server/admin"
)

// stubCatalog satisfies ProviderCatalogReader via a fixed slice.
type stubCatalog struct{ items []providers.RuntimeMetadata }

func (s *stubCatalog) List() []providers.RuntimeMetadata { return s.items }

func TestProviderHealthHandler_StatusDerivedFromEnabled(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	catalog := &stubCatalog{items: []providers.RuntimeMetadata{
		{
			Name:      "openai-primary",
			Type:      "openai",
			Kind:      "cloud",
			Enabled:   true,
			CheckedAt: now,
		},
		{
			Name:       "local-llm",
			Type:       "openai_compatible",
			Kind:       "local",
			Enabled:    false,
			SkipReason: "missing_api_key",
			CheckedAt:  now,
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/providers/health", nil)
	w := httptest.NewRecorder()
	admin.ProviderHealthHandler(catalog)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Providers []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			SkipReason string `json:"skip_reason"`
			Kind       string `json:"kind"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(resp.Providers))
	}

	if resp.Providers[0].Name != "openai-primary" {
		t.Errorf("providers[0].name: got %q, want openai-primary", resp.Providers[0].Name)
	}
	if resp.Providers[0].Status != "enabled" {
		t.Errorf("providers[0].status: got %q, want enabled", resp.Providers[0].Status)
	}
	if resp.Providers[0].Kind != "cloud" {
		t.Errorf("providers[0].kind: got %q, want cloud", resp.Providers[0].Kind)
	}

	if resp.Providers[1].Name != "local-llm" {
		t.Errorf("providers[1].name: got %q, want local-llm", resp.Providers[1].Name)
	}
	if resp.Providers[1].Status != "disabled" {
		t.Errorf("providers[1].status: got %q, want disabled", resp.Providers[1].Status)
	}
	if resp.Providers[1].SkipReason != "missing_api_key" {
		t.Errorf("providers[1].skip_reason: got %q, want missing_api_key", resp.Providers[1].SkipReason)
	}
	if resp.Providers[1].Kind != "local" {
		t.Errorf("providers[1].kind: got %q, want local", resp.Providers[1].Kind)
	}
}

func TestProviderHealthHandler_CheckedAtPresent(t *testing.T) {
	checkedAt := time.Now().UTC().Truncate(time.Second)
	catalog := &stubCatalog{items: []providers.RuntimeMetadata{
		{Name: "p1", Type: "openai", Enabled: true, CheckedAt: checkedAt},
	}}

	req := httptest.NewRequest(http.MethodGet, "/providers/health", nil)
	w := httptest.NewRecorder()
	admin.ProviderHealthHandler(catalog)(w, req)

	var resp struct {
		Providers []struct {
			CheckedAt time.Time `json:"checked_at"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Providers[0].CheckedAt.IsZero() {
		t.Error("expected checked_at to be non-zero")
	}
}

func TestProviderHealthHandler_EmptyCatalog(t *testing.T) {
	catalog := &stubCatalog{}

	req := httptest.NewRequest(http.MethodGet, "/providers/health", nil)
	w := httptest.NewRecorder()
	admin.ProviderHealthHandler(catalog)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Providers []any `json:"providers"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Providers) != 0 {
		t.Errorf("expected 0 providers, got %d", len(resp.Providers))
	}
}
