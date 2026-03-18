package providers

import (
	"context"
	"net/http"
)

type ResponseMeta struct {
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

type ErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type,omitempty"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

type Provider interface {
	Name() string
	Do(ctx context.Context, req *http.Request) (*http.Response, error)

	// Parse normalized usage/model info from a successful provider response body.
	ParseResponseMeta(body []byte) (ResponseMeta, error)

	// Normalize upstream/provider-specific error bodies into one gateway shape.
	NormalizeError(statusCode int, body []byte) ([]byte, error)
}
