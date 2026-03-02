package providers

import (
	"context"
	"net/http"
)

type Provider interface {
	Name() string

	// Phase A: raw HTTP forwarding.
	// Later you can add typed methods (ChatCompletions, Embeddings...) if you want.
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
}
