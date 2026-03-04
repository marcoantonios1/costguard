package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/marcoantonios1/costguard/internal/providers"
)

type Router interface {
	PickProvider(model string) string
}

type Gateway struct {
	router Router
	reg    *providers.Registry
}

type Deps struct {
	Router   Router
	Registry *providers.Registry
}

func New(d Deps) (*Gateway, error) {
	if d.Router == nil {
		return nil, fmt.Errorf("router is nil")
	}
	if d.Registry == nil {
		return nil, fmt.Errorf("registry is nil")
	}
	return &Gateway{router: d.Router, reg: d.Registry}, nil
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

	p, err := g.reg.Get(providerName)
	if err != nil {
		return nil, err
	}

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	return p.Do(r.Context(), r)
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
