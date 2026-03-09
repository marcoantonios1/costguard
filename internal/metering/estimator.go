package metering

func EstimateCost(u Usage) (float64, bool) {
	if u.CacheHit {
		return 0, true
	}

	normalizedProvider := NormalizeProvider(u.Provider)
	providerPrices, ok := Prices[normalizedProvider]
	if !ok {
		return 0, false
	}

	normalized := NormalizeModel(normalizedProvider, u.Model)

	price, ok := providerPrices[normalized]
	if !ok {
		return 0, false
	}

	inputCost := (float64(u.PromptTokens) / 1_000_000) * price.InputPer1M
	outputCost := (float64(u.CompletionTokens) / 1_000_000) * price.OutputPer1M

	return inputCost + outputCost, true
}
