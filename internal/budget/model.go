package budget

import "time"

type Status struct {
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	MonthlyBudgetUSD   float64   `json:"monthly_budget_usd"`
	CurrentSpendUSD    float64   `json:"current_spend_usd"`
	PercentageUsed     float64   `json:"percentage_used"`
	RemainingBudgetUSD float64   `json:"remaining_budget_usd"`
	Exceeded           bool      `json:"exceeded"`
}
