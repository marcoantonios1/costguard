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

// stubPriority implements ProviderPriority with a fixed priority map.
type stubPriority struct {
	priorities map[string]int
}

func (s *stubPriority) Priority(provider string) int {
	return s.priorities[provider]
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

// --- priority path tests ---

func TestPickProvider_Priority_HigherWins(t *testing.T) {
	// exact_mapping resolves gpt-4o → openai_primary (priority 95)
	// default is anthropic_primary (priority 100)
	// anthropic_primary should win because priority 100 > 95
	r := newRouter(router.Config{
		DefaultProvider: "anthropic_primary",
		ModelToProvider: map[string]string{"gpt-4o": "openai_primary"},
		AvailableProviders: map[string]bool{
			"openai_primary":    true,
			"anthropic_primary": true,
		},
		Priority: &stubPriority{priorities: map[string]int{
			"openai_primary":    95,
			"anthropic_primary": 100,
		}},
	})
	got := r.PickProvider("gpt-4o")
	if got != "anthropic_primary" {
		t.Errorf("expected anthropic_primary (higher priority), got %q", got)
	}
}

func TestPickProvider_Priority_LexicographicTiebreaker(t *testing.T) {
	// exact_mapping → "beta_provider" (priority 80)
	// default        → "alpha_provider" (priority 80)
	// equal priority: "alpha_provider" < "beta_provider" lexicographically → alpha wins
	r := newRouter(router.Config{
		DefaultProvider: "alpha_provider",
		ModelToProvider: map[string]string{"some-model": "beta_provider"},
		AvailableProviders: map[string]bool{
			"alpha_provider": true,
			"beta_provider":  true,
		},
		Priority: &stubPriority{priorities: map[string]int{
			"alpha_provider": 80,
			"beta_provider":  80,
		}},
	})
	got := r.PickProvider("some-model")
	if got != "alpha_provider" {
		t.Errorf("expected alpha_provider (lexicographic tiebreaker), got %q", got)
	}
}

func TestPickProvider_Priority_SingleCandidate_ReasonExactMapping(t *testing.T) {
	r := newRouter(router.Config{
		DefaultProvider: "openai_primary",
		ModelToProvider: map[string]string{"gpt-4o": "openai_primary"},
		AvailableProviders: map[string]bool{
			"openai_primary": true,
		},
		Priority: &stubPriority{priorities: map[string]int{
			"openai_primary": 95,
		}},
	})
	// Only one candidate (default and exact_mapping both resolve to same provider)
	got := r.PickProvider("gpt-4o")
	if got != "openai_primary" {
		t.Errorf("expected openai_primary, got %q", got)
	}
}

func TestPickProvider_Priority_NilLegacyBehaviorUnchanged(t *testing.T) {
	// Priority=nil → legacy order: exact_mapping wins over default regardless of
	// what priority values a catalog might report.
	r := newRouter(router.Config{
		DefaultProvider: "anthropic_primary",
		ModelToProvider: map[string]string{"gpt-4o": "openai_primary"},
		AvailableProviders: map[string]bool{
			"openai_primary":    true,
			"anthropic_primary": true,
		},
		Priority: nil,
	})
	// With legacy order, exact_mapping wins immediately.
	got := r.PickProvider("gpt-4o")
	if got != "openai_primary" {
		t.Errorf("expected openai_primary (legacy exact_mapping wins), got %q", got)
	}
}

func TestPickProvider_Priority_NoCandidates_ReturnsEmpty(t *testing.T) {
	r := newRouter(router.Config{
		DefaultProvider:    "openai_primary",
		AvailableProviders: map[string]bool{}, // nothing available
		Priority:           &stubPriority{},
	})
	got := r.PickProvider("gpt-4o")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestPickProvider_Priority_CatalogBlocksCandidate_FallsToNext(t *testing.T) {
	// exact_mapping → anthropic_primary, but catalog blocks it for gpt-4o
	// matcher       → openai_primary (unconstrained), allowed
	// anthropic_primary should not appear as a candidate
	r := newRouter(router.Config{
		DefaultProvider: "anthropic_primary",
		ModelToProvider: map[string]string{"gpt-4o": "anthropic_primary"},
		AvailableProviders: map[string]bool{
			"anthropic_primary": true,
			"openai_primary":    true,
		},
		Catalog: &stubCatalog{supported: map[string]map[string]bool{
			"anthropic_primary": {"claude-sonnet-4-6": true}, // blocks gpt-4o
		}},
		Priority: &stubPriority{priorities: map[string]int{
			"anthropic_primary": 100,
			"openai_primary":    95,
		}},
	})
	got := r.PickProvider("gpt-4o")
	// anthropic_primary blocked by catalog; openai_primary wins via matcher
	if got != "openai_primary" {
		t.Errorf("expected openai_primary, got %q", got)
	}
}
