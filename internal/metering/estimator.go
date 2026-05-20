package metering

var configPrices map[string]map[string]Price

// SetConfigPrices registers pricing from the application config. Config prices
// take precedence over the built-in Prices table in EstimateCost.
func SetConfigPrices(prices map[string]map[string]Price) {
	configPrices = prices
}

func EstimateCost(u Usage) (float64, bool) {
	if u.CacheHit {
		return 0, true
	}

	normalizedProvider := NormalizeProvider(u.Provider)
	normalized := NormalizeModel(normalizedProvider, u.Model)

	if configPrices != nil {
		if providerPrices, ok := configPrices[normalizedProvider]; ok {
			if price, ok := providerPrices[normalized]; ok {
				inputCost := (float64(u.PromptTokens) / 1_000_000) * price.InputPer1M
				outputCost := (float64(u.CompletionTokens) / 1_000_000) * price.OutputPer1M
				return inputCost + outputCost, true
			}
		}
	}

	providerPrices, ok := Prices[normalizedProvider]
	if !ok {
		return 0, false
	}

	price, ok := providerPrices[normalized]
	if !ok {
		return 0, false
	}

	inputCost := (float64(u.PromptTokens) / 1_000_000) * price.InputPer1M
	outputCost := (float64(u.CompletionTokens) / 1_000_000) * price.OutputPer1M

	return inputCost + outputCost, true
}
