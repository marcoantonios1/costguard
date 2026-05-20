package router_test

import (
	"testing"

	"github.com/marcoantonios1/costguard/internal/router"
)

// stubCatalog implements ModelCatalog with a fixed allow/deny set.
type stubCatalog struct {
	// supported is providerName → set of allowed model IDs.
	// If a provider key is absent, ModelSupported returns true (unconstrained).
	supported map[string]map[string]bool
}

func (s *stubCatalog) ModelSupported(provider, model string) bool {
	models, ok := s.supported[provider]
	if !ok {
		return true // unconstrained
	}
	return models[model]
}

func newRouter(cfg router.Config) *router.Router {
	return router.New(cfg)
}

func TestPickProvider_NilCatalog_NoRegression(t *testing.T) {
	r := newRouter(router.Config{
		DefaultProvider: "openai_primary",
		AvailableProviders: map[string]bool{
			"openai_primary": true,
		},
		Catalog: nil,
	})
	got := r.PickProvider("gpt-4o")
	if got != "openai_primary" {
		t.Errorf("expected openai_primary, got %q", got)
	}
}

func TestPickProvider_CatalogAllows_ExactMapping(t *testing.T) {
	r := newRouter(router.Config{
		DefaultProvider:    "fallback",
		ModelToProvider:    map[string]string{"gpt-4o": "openai_primary"},
		AvailableProviders: map[string]bool{"openai_primary": true, "fallback": true},
		Catalog: &stubCatalog{supported: map[string]map[string]bool{
			"openai_primary": {"gpt-4o": true},
		}},
	})
	got := r.PickProvider("gpt-4o")
	if got != "openai_primary" {
		t.Errorf("expected openai_primary, got %q", got)
	}
}

func TestPickProvider_CatalogBlocks_ExactMapping_FallsToDefault(t *testing.T) {
	r := newRouter(router.Config{
		DefaultProvider: "fallback",
		ModelToProvider: map[string]string{"gpt-4o": "openai_primary"},
		AvailableProviders: map[string]bool{
			"openai_primary": true,
			"fallback":       true,
		},
		Catalog: &stubCatalog{supported: map[string]map[string]bool{
			"openai_primary": {}, // blocks gpt-4o
		}},
	})
	got := r.PickProvider("gpt-4o")
	// openai_primary is blocked by catalog → falls through to default (unconstrained)
	if got != "fallback" {
		t.Errorf("expected fallback, got %q", got)
	}
}

func TestPickProvider_CatalogBlocks_Matcher_FallsToDefault(t *testing.T) {
	r := newRouter(router.Config{
		DefaultProvider: "fallback",
		AvailableProviders: map[string]bool{
			"openai_primary": true,
			"fallback":       true,
		},
		Catalog: &stubCatalog{supported: map[string]map[string]bool{
			"openai_primary": {}, // blocks everything
		}},
	})
	// gpt-4o matches "openai_primary" via matcher, but catalog blocks it
	got := r.PickProvider("gpt-4o")
	if got != "fallback" {
		t.Errorf("expected fallback, got %q", got)
	}
}

func TestPickProvider_CatalogBlocks_Default_ReturnsEmpty(t *testing.T) {
	r := newRouter(router.Config{
		DefaultProvider:    "openai_primary",
		AvailableProviders: map[string]bool{"openai_primary": true},
		Catalog: &stubCatalog{supported: map[string]map[string]bool{
			"openai_primary": {}, // blocks the default too
		}},
	})
	got := r.PickProvider("unknown-model")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestPickProvider_UnconstrainedCatalog_AllowsAll(t *testing.T) {
	r := newRouter(router.Config{
		DefaultProvider:    "openai_primary",
		AvailableProviders: map[string]bool{"openai_primary": true},
		Catalog:            &stubCatalog{}, // all unconstrained
	})
	got := r.PickProvider("anything")
	if got != "openai_primary" {
		t.Errorf("expected openai_primary, got %q", got)
	}
}
