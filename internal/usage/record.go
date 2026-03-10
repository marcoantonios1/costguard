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
	Path             string
	StatusCode       int
}
