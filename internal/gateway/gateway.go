package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/marcoantonios1/costguard/internal/logging"
	"github.com/marcoantonios1/costguard/internal/providers"
	"github.com/marcoantonios1/costguard/internal/server"
)

type Router interface {
	PickProvider(model string) string
}

type Gateway struct {
	router Router
	reg    *providers.Registry
	log    *logging.Log

	fallback string
}

type Deps struct {
	Router   Router
	Registry *providers.Registry
	Log      *logging.Log

	FallbackProvider string
}

func New(d Deps) (*Gateway, error) {
	if d.Router == nil {
		return nil, fmt.Errorf("router is nil")
	}
	if d.Registry == nil {
		return nil, fmt.Errorf("registry is nil")
	}
	return &Gateway{
		router:   d.Router,
		reg:      d.Registry,
		log:      d.Log,
		fallback: d.FallbackProvider,
	}, nil
}

func (g *Gateway) Proxy(r *http.Request) (*http.Response, error) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	model := extractModel(bodyBytes)
	providerName := g.router.PickProvider(model)

	resp, err := g.callProvider(r, bodyBytes, providerName)
	if err == nil {
		return resp, nil
	}

	shouldTryFallback := g.fallback != "" && g.fallback != providerName
	if shouldTryFallback {
		if g.log != nil {
			g.log.Error("provider_failed_try_fallback", map[string]any{
				"request_id": server.RequestIDFromContext(r.Context()),
				"primary":    providerName,
				"fallback":   g.fallback,
				"err":        err.Error(),
			})
		}
		return g.callProvider(r, bodyBytes, g.fallback)
	}

	return nil, err
}

func (g *Gateway) callProvider(r *http.Request, bodyBytes []byte, providerName string) (*http.Response, error) {
	p, err := g.reg.Get(providerName)
	if err != nil {
		return nil, err
	}

	// refresh body for provider call
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	resp, err := p.Do(r.Context(), r)
	if err != nil {
		return nil, err
	}

	// Optional: only fallback on 5xx (not on 4xx like auth/quota)
	if resp.StatusCode >= 500 && g.fallback != "" && providerName != g.fallback {
		// close body because we're not returning it
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream_5xx status=%d provider=%s", resp.StatusCode, providerName)
	}

	return resp, nil
}

func extractModel(body []byte) string {
	var v struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return ""
	}
	return v.Model
}
