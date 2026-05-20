package metering

import "testing"

func TestInputPricePer1M_KnownModel(t *testing.T) {
	price, ok := InputPricePer1M("anthropic", "claude-haiku-4-5")
	if !ok {
		t.Fatal("expected ok=true for known model")
	}
	if price != 1.00 {
		t.Errorf("expected 1.00, got %f", price)
	}
}

func TestInputPricePer1M_VersionSuffixNormalized(t *testing.T) {
	// "claude-haiku-4-5-20251001" normalizes to "claude-haiku-4-5"
	price, ok := InputPricePer1M("anthropic_primary", "claude-haiku-4-5-20251001")
	if !ok {
		t.Fatal("expected ok=true after normalization")
	}
	if price != 1.00 {
		t.Errorf("expected 1.00, got %f", price)
	}
}

func TestInputPricePer1M_UnknownProvider(t *testing.T) {
	price, ok := InputPricePer1M("unknown_provider", "unknown-model")
	if ok {
		t.Error("expected ok=false for unknown provider")
	}
	if price != 0 {
		t.Errorf("expected 0, got %f", price)
	}
}

func TestInputPricePer1M_UnknownModel(t *testing.T) {
	price, ok := InputPricePer1M("openai", "gpt-99-turbo")
	if ok {
		t.Error("expected ok=false for unknown model")
	}
	if price != 0 {
		t.Errorf("expected 0, got %f", price)
	}
}

func TestInputPricePer1M_ConfigOverrideTakesPrecedence(t *testing.T) {
	SetConfigPrices(map[string]map[string]Price{
		"openai": {"gpt-4o-mini": {InputPer1M: 99.0, OutputPer1M: 1.0}},
	})
	defer SetConfigPrices(nil)

	price, ok := InputPricePer1M("openai_primary", "gpt-4o-mini")
	if !ok {
		t.Fatal("expected ok=true with config override")
	}
	if price != 99.0 {
		t.Errorf("expected 99.0 (config override), got %f", price)
	}
}
