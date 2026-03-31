package usage

import "time"

type Record struct {
	RequestID        string
	Timestamp        time.Time
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCostUSD float64
	PriceFound       bool
	CacheHit         bool
	Team             string
	Project          string
	User             string
	Agent            string
	Path             string
	StatusCode       int
}

type TeamSpend struct {
	Team  string  `json:"team"`
	Spend float64 `json:"spend_usd"`
}

type ProjectSpend struct {
	Project string  `json:"project"`
	Spend   float64 `json:"spend_usd"`
}

type ModelSpend struct {
	Model string  `json:"model"`
	Spend float64 `json:"spend_usd"`
}

type ProviderSpend struct {
	Provider string  `json:"provider"`
	Spend    float64 `json:"spend_usd"`
}

type AgentSpend struct {
	Agent string  `json:"agent"`
	Spend float64 `json:"spend_usd"`
}

type Summary struct {
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	TotalSpendUSD float64   `json:"total_spend_usd"`
}
