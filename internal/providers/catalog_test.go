package providers_test

import (
	"slices"
	"testing"

	"github.com/marcoantonios1/costguard/internal/providers"
)

func newCatalog(items ...providers.RuntimeMetadata) *providers.Catalog {
	c := providers.NewCatalog()
	for _, md := range items {
		c.Set(md.Name, md)
	}
	return c
}

func TestModelSupported_Unconstrained(t *testing.T) {
	c := newCatalog(providers.RuntimeMetadata{
		Name:    "openai_primary",
		Enabled: true,
		// SupportedModels is nil → unconstrained
	})
	if !c.ModelSupported("openai_primary", "gpt-4o") {
		t.Error("unconstrained provider should support any model")
	}
	if !c.ModelSupported("openai_primary", "anything") {
		t.Error("unconstrained provider should support any model")
	}
}

func TestModelSupported_Constrained(t *testing.T) {
	c := newCatalog(providers.RuntimeMetadata{
		Name:            "anthropic_primary",
		Enabled:         true,
		SupportedModels: []string{"claude-sonnet-4-6", "claude-opus-4-6"},
	})

	if !c.ModelSupported("anthropic_primary", "claude-sonnet-4-6") {
		t.Error("expected claude-sonnet-4-6 to be supported")
	}
	if c.ModelSupported("anthropic_primary", "gpt-4o") {
		t.Error("expected gpt-4o to NOT be supported by anthropic_primary")
	}
}

func TestModelSupported_UnknownProvider(t *testing.T) {
	c := newCatalog()
	if c.ModelSupported("nonexistent", "gpt-4o") {
		t.Error("unknown provider should return false")
	}
}

func TestSupportsModel_ReturnsExplicitProviders(t *testing.T) {
	c := newCatalog(
		providers.RuntimeMetadata{
			Name:            "anthropic_primary",
			Enabled:         true,
			SupportedModels: []string{"claude-sonnet-4-6"},
		},
		providers.RuntimeMetadata{
			Name:    "openai_primary",
			Enabled: true,
			// unconstrained — must NOT appear in SupportsModel results
		},
	)

	got := c.SupportsModel("claude-sonnet-4-6")
	if len(got) != 1 || got[0] != "anthropic_primary" {
		t.Errorf("SupportsModel(claude-sonnet-4-6) = %v, want [anthropic_primary]", got)
	}
}

func TestSupportsModel_ExcludesDisabled(t *testing.T) {
	c := newCatalog(
		providers.RuntimeMetadata{
			Name:            "anthropic_primary",
			Enabled:         false, // disabled
			SupportedModels: []string{"claude-sonnet-4-6"},
		},
	)
	got := c.SupportsModel("claude-sonnet-4-6")
	if len(got) != 0 {
		t.Errorf("disabled provider must not appear in SupportsModel, got %v", got)
	}
}

func TestSupportsModel_MultipleProviders(t *testing.T) {
	c := newCatalog(
		providers.RuntimeMetadata{
			Name:            "anthropic_primary",
			Enabled:         true,
			SupportedModels: []string{"claude-sonnet-4-6"},
		},
		providers.RuntimeMetadata{
			Name:            "anthropic_backup",
			Enabled:         true,
			SupportedModels: []string{"claude-sonnet-4-6", "claude-opus-4-6"},
		},
	)

	got := c.SupportsModel("claude-sonnet-4-6")
	if len(got) != 2 {
		t.Fatalf("expected 2 providers, got %v", got)
	}
	if !slices.Contains(got, "anthropic_primary") || !slices.Contains(got, "anthropic_backup") {
		t.Errorf("unexpected result: %v", got)
	}
}

func TestCatalogPriority_Known(t *testing.T) {
	c := newCatalog(providers.RuntimeMetadata{
		Name:     "anthropic_primary",
		Enabled:  true,
		Priority: 100,
	})
	if got := c.Priority("anthropic_primary"); got != 100 {
		t.Errorf("Priority(anthropic_primary) = %d, want 100", got)
	}
}

func TestCatalogPriority_Unknown(t *testing.T) {
	c := newCatalog()
	if got := c.Priority("nonexistent"); got != 0 {
		t.Errorf("Priority(nonexistent) = %d, want 0", got)
	}
}

func TestCatalogPriority_Zero(t *testing.T) {
	c := newCatalog(providers.RuntimeMetadata{Name: "local_ollama", Enabled: true})
	if got := c.Priority("local_ollama"); got != 0 {
		t.Errorf("Priority(local_ollama) = %d, want 0", got)
	}
}

func TestSupportsModel_NotListed(t *testing.T) {
	c := newCatalog(providers.RuntimeMetadata{
		Name:            "anthropic_primary",
		Enabled:         true,
		SupportedModels: []string{"claude-sonnet-4-6"},
	})
	got := c.SupportsModel("gpt-4o")
	if len(got) != 0 {
		t.Errorf("expected no providers for gpt-4o, got %v", got)
	}
}
