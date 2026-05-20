package router

import "github.com/marcoantonios1/costguard/internal/metering"

// MeteringCostOracle adapts metering.InputPricePer1M to the CostOracle interface.
type MeteringCostOracle struct{}

func (MeteringCostOracle) InputPricePer1M(providerName, modelID string) (float64, bool) {
	return metering.InputPricePer1M(providerName, modelID)
}
