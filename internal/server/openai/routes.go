package openai

import "net/http"

// Gateway is the minimal dependency the OpenAI-compatible HTTP layer needs.
// It lets the gateway own routing/cache/metering/provider selection.
type Gateway interface {
	Proxy(r *http.Request) (*http.Response, error)
}

type Deps struct {
	Gateway Gateway
}

// Register attaches OpenAI-compatible endpoints to the mux.
// Phase A: start with /v1/chat/completions. Add more later.
func Register(mux *http.ServeMux, d Deps) {
	h := &handler{gw: d.Gateway}

	mux.HandleFunc("/v1/chat/completions", h.chatCompletions)
	// Future:
	// mux.HandleFunc("/v1/embeddings", h.embeddings)
	// mux.HandleFunc("/v1/responses", h.responses)
}