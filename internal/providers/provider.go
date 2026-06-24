package providers

import (
	"context"
	"net/http"
)

// Canonical error categories surfaced in logs and client-facing error bodies.
const (
	ErrCategoryAuth               = "auth"
	ErrCategoryInvalidRequest     = "invalid_request"
	ErrCategoryRateLimit          = "rate_limit"
	ErrCategoryProviderUnavailable = "provider_unavailable"
	ErrCategoryUpstreamFailure    = "upstream_failure"
)

type ResponseMeta struct {
	Model                    string
	PromptTokens             int // base (non-cache) input tokens only
	CompletionTokens         int
	TotalTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}

type ErrorBody struct {
	Error struct {
		Message  string `json:"message"`
		Type     string `json:"type,omitempty"`
		Code     string `json:"code,omitempty"`
		Category string `json:"category,omitempty"`
	} `json:"error"`
}

// ErrorCategory derives the canonical category from a normalized error type
// string and the upstream HTTP status code. Type takes precedence over status.
func ErrorCategory(errType string, statusCode int) string {
	switch errType {
	case "authentication_error", "permission_error":
		return ErrCategoryAuth
	case "invalid_request_error":
		return ErrCategoryInvalidRequest
	case "rate_limit_error":
		return ErrCategoryRateLimit
	}
	switch statusCode {
	case 401, 403:
		return ErrCategoryAuth
	case 400, 422:
		return ErrCategoryInvalidRequest
	case 429:
		return ErrCategoryRateLimit
	case 502, 503, 504:
		return ErrCategoryProviderUnavailable
	default:
		return ErrCategoryUpstreamFailure
	}
}

type Provider interface {
	Name() string
	// Family returns the provider's API family ("anthropic", "openai", "gemini",
	// "openaicompat"). Used for provider-specific logic such as vision token estimation.
	Family() string
	Do(ctx context.Context, req *http.Request) (*http.Response, error)

	// Parse normalized usage/model info from a successful provider response body.
	ParseResponseMeta(body []byte) (ResponseMeta, error)

	// Normalize upstream/provider-specific error bodies into one gateway shape.
	NormalizeError(statusCode int, body []byte) ([]byte, error)
}
