package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/health"
	"github.com/marcoantonios1/costguard/internal/providers"
)

// HealthStatsReader surfaces per-provider health snapshots from the tracker.
// *health.Tracker satisfies this interface.
type HealthStatsReader interface {
	Snapshot(provider string) health.Snapshot
}

type ProviderHealthEntry struct {
	Name               string    `json:"name"`
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	SkipReason         string    `json:"skip_reason,omitempty"`
	Kind               string    `json:"kind,omitempty"`
	BaseURL            string    `json:"base_url"`
	HasAPIKey          bool      `json:"has_api_key"`
	SupportsTools      bool      `json:"supports_tools"`
	SupportsStreaming   bool      `json:"supports_streaming"`
	SupportsVision     bool      `json:"supports_vision"`
	SupportsEmbeddings bool      `json:"supports_embeddings"`
	Priority           int       `json:"priority"`
	Tags               []string  `json:"tags"`
	CheckedAt          time.Time `json:"checked_at"`
	// Live health stats; SuccessRate and AvgLatencyMS are -1 when no data.
	Total        int     `json:"total"`
	Successes    int     `json:"successes"`
	Failures     int     `json:"failures"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMS float64 `json:"avg_latency_ms"`
	LastError    string  `json:"last_error,omitempty"`
}

func toHealthEntry(m providers.RuntimeMetadata) ProviderHealthEntry {
	status := "enabled"
	if !m.Enabled {
		status = "disabled"
	}
	return ProviderHealthEntry{
		Name:               m.Name,
		Type:               m.Type,
		Status:             status,
		SkipReason:         m.SkipReason,
		Kind:               m.Kind,
		BaseURL:            m.BaseURL,
		HasAPIKey:          m.HasAPIKey,
		SupportsTools:      m.SupportsTools,
		SupportsStreaming:   m.SupportsStreaming,
		SupportsVision:     m.SupportsVision,
		SupportsEmbeddings: m.SupportsEmbeddings,
		Priority:           m.Priority,
		Tags:               m.Tags,
		CheckedAt:          m.CheckedAt,
		SuccessRate:        -1,
		AvgLatencyMS:       -1,
	}
}

func ProviderHealthHandler(catalog ProviderCatalogReader, stats HealthStatsReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list := catalog.List()
		entries := make([]ProviderHealthEntry, len(list))
		for i, m := range list {
			e := toHealthEntry(m)
			if stats != nil {
				snap := stats.Snapshot(m.Name)
				e.Total = snap.Total
				e.Successes = snap.Successes
				e.Failures = snap.Failures
				e.SuccessRate = snap.SuccessRate
				e.AvgLatencyMS = snap.AvgLatencyMS
				e.LastError = snap.LastError
			}
			entries[i] = e
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providers": entries,
		})
	}
}
