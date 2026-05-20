package metering

// InputPricePer1M returns the input cost per 1M tokens for the given provider
// and model. It applies the same normalization and config-override logic as
// EstimateCost. Returns (price, true) when found; (0, false) when unknown.
func InputPricePer1M(providerName, modelID string) (float64, bool) {
	normalizedProvider := NormalizeProvider(providerName)
	normalized := NormalizeModel(normalizedProvider, modelID)

	if configPrices != nil {
		if providerPrices, ok := configPrices[normalizedProvider]; ok {
			if price, ok := providerPrices[normalized]; ok {
				return price.InputPer1M, true
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
	return price.InputPer1M, true
}
