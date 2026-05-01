package providers

import "time"

type RuntimeMetadata struct {
	Name               string    `json:"name"`
	Type               string    `json:"type"`
	BaseURL            string    `json:"base_url"`
	AuthRequired       bool      `json:"auth_required"`
	HasAPIKey          bool      `json:"has_api_key"`
	Kind               string    `json:"kind"`
	SupportsTools      bool      `json:"supports_tools"`
	SupportsStreaming  bool      `json:"supports_streaming"`
	SupportsVision     bool      `json:"supports_vision"`
	SupportsEmbeddings bool      `json:"supports_embeddings"`
	Priority           int       `json:"priority"`
	Tags               []string  `json:"tags"`
	Enabled            bool      `json:"enabled"`
	SkipReason         string    `json:"skip_reason,omitempty"`
	CheckedAt          time.Time `json:"checked_at"`
}
