package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/marcoantonios1/costguard/internal/providers"
)

type ProviderHealthEntry struct {
	Name               string    `json:"name"`
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	SkipReason         string    `json:"skip_reason,omitempty"`
	Kind               string    `json:"kind,omitempty"`
	BaseURL            string    `json:"base_url"`
	HasAPIKey          bool      `json:"has_api_key"`
	SupportsTools      bool      `json:"supports_tools"`
	SupportsStreaming  bool      `json:"supports_streaming"`
	SupportsVision     bool      `json:"supports_vision"`
	SupportsEmbeddings bool      `json:"supports_embeddings"`
	Priority           int       `json:"priority"`
	Tags               []string  `json:"tags"`
	CheckedAt          time.Time `json:"checked_at"`
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
		SupportsStreaming:  m.SupportsStreaming,
		SupportsVision:     m.SupportsVision,
		SupportsEmbeddings: m.SupportsEmbeddings,
		Priority:           m.Priority,
		Tags:               m.Tags,
		CheckedAt:          m.CheckedAt,
	}
}

func ProviderHealthHandler(catalog ProviderCatalogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list := catalog.List()
		entries := make([]ProviderHealthEntry, len(list))
		for i, m := range list {
			entries[i] = toHealthEntry(m)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providers": entries,
		})
	}
}
