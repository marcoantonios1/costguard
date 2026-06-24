package metering

import "testing"

// claude-haiku-4-5: InputPer1M=1.00, OutputPer1M=5.00
// Naive all-input cost = (total_input / 1M) * 1.00

func TestEstimateCost_CacheReadHeavy(t *testing.T) {
	// Small base input, large cache-read count.
	// Naive cost would treat all input tokens at 1.0x.
	// With cache reads at 0.1x, real cost should be substantially lower.
	u := Usage{
		Provider:             "anthropic",
		Model:                "claude-haiku-4-5",
		PromptTokens:         1_000,   // base input
		CacheReadInputTokens: 99_000,  // cache reads — 99% of input volume
		CompletionTokens:     500,
		TotalTokens:          100_500,
	}

	cost, ok := EstimateCost(u)
	if !ok {
		t.Fatal("expected price to be found")
	}

	naiveCost := (float64(u.PromptTokens+u.CacheReadInputTokens) / 1_000_000) * 1.00
	naiveCost += (float64(u.CompletionTokens) / 1_000_000) * 5.00

	if cost >= naiveCost {
		t.Errorf("cache-read-heavy cost %.6f should be below naive cost %.6f (0.1x multiplier)", cost, naiveCost)
	}

	// Cache reads should be ~0.1x of their naive contribution.
	// With 99k cache-read tokens at $1/M base:
	//   naive cache portion = 99_000/1M * 1.00 = 0.099
	//   actual cache portion = 99_000/1M * 1.00 * 0.1 = 0.0099
	// Savings ≈ 90% of the cache portion → cost should be well below naive.
	savingsFraction := (naiveCost - cost) / naiveCost
	if savingsFraction < 0.80 {
		t.Errorf("expected >80%% savings from cache reads, got %.1f%%", savingsFraction*100)
	}
}

func TestEstimateCost_CacheWriteHeavy(t *testing.T) {
	// Large cache-creation count.
	// Cache writes are 1.25x, so cost should exceed the naive all-input estimate.
	u := Usage{
		Provider:                 "anthropic",
		Model:                    "claude-haiku-4-5",
		PromptTokens:             1_000,  // base input
		CacheCreationInputTokens: 99_000, // cache writes — 99% of input volume
		CompletionTokens:         500,
		TotalTokens:              100_500,
	}

	cost, ok := EstimateCost(u)
	if !ok {
		t.Fatal("expected price to be found")
	}

	naiveCost := (float64(u.PromptTokens+u.CacheCreationInputTokens) / 1_000_000) * 1.00
	naiveCost += (float64(u.CompletionTokens) / 1_000_000) * 5.00

	if cost <= naiveCost {
		t.Errorf("cache-write-heavy cost %.6f should exceed naive cost %.6f (1.25x multiplier)", cost, naiveCost)
	}

	// Cache writes at 1.25x add 25% on top of the naive cache portion.
	// Extra fraction over naive ≈ 0.25 * (cache_write_input / total_input).
	premiumFraction := (cost - naiveCost) / naiveCost
	if premiumFraction < 0.20 {
		t.Errorf("expected >20%% premium from cache writes, got %.1f%%", premiumFraction*100)
	}
}

func TestEstimateCost_NoCacheFields_Unchanged(t *testing.T) {
	// When cache fields are zero, cost must be identical to the pre-change
	// formula: (prompt/1M)*inputPrice + (completion/1M)*outputPrice.
	u := Usage{
		Provider:         "anthropic",
		Model:            "claude-haiku-4-5",
		PromptTokens:     10_000,
		CompletionTokens: 2_000,
		TotalTokens:      12_000,
	}

	cost, ok := EstimateCost(u)
	if !ok {
		t.Fatal("expected price to be found")
	}

	want := (10_000.0/1_000_000)*1.00 + (2_000.0/1_000_000)*5.00
	if cost != want {
		t.Errorf("expected %.6f, got %.6f", want, cost)
	}
}

func TestEstimateCost_ConfigPrices_CacheFields(t *testing.T) {
	SetConfigPrices(map[string]map[string]Price{
		"anthropic": {"claude-haiku-4-5": {InputPer1M: 2.00, OutputPer1M: 10.00}},
	})
	defer SetConfigPrices(nil)

	u := Usage{
		Provider:             "anthropic",
		Model:                "claude-haiku-4-5",
		PromptTokens:         1_000,
		CacheReadInputTokens: 99_000,
		CompletionTokens:     500,
		TotalTokens:          100_500,
	}

	cost, ok := EstimateCost(u)
	if !ok {
		t.Fatal("expected price to be found via configPrices")
	}

	naiveCost := (float64(u.PromptTokens+u.CacheReadInputTokens) / 1_000_000) * 2.00
	naiveCost += (float64(u.CompletionTokens) / 1_000_000) * 10.00

	if cost >= naiveCost {
		t.Errorf("configPrices branch: cache-read cost %.6f should be below naive %.6f", cost, naiveCost)
	}
}
