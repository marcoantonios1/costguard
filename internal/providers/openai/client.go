package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/providers"
)

type ClientConfig struct {
	Name    string
	BaseURL string
	APIKey  string
	Org     string
	Project string
	Timeout time.Duration
}

type Client struct {
	name    string
	base    *url.URL
	apiKey  string
	org     string
	project string
	hc      *http.Client
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("openai Client name is required")
	}
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.openai.com"
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	to := cfg.Timeout
	if to == 0 {
		to = 60 * time.Second
	}
	return &Client{
		name:    cfg.Name,
		base:    u,
		apiKey:  cfg.APIKey,
		org:     cfg.Org,
		project: cfg.Project,
		hc:      &http.Client{Timeout: to},
	}, nil
}

func (a *Client) Name() string { return a.name }

// Do forwards the incoming request to the OpenAI upstream API.
// It preserves method, path, query, and body.
func (a *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}

	// Build upstream URL: base + path + query
	upstreamURL := *a.base
	upstreamURL.Path = joinURLPath(a.base.Path, req.URL.Path)
	upstreamURL.RawQuery = req.URL.RawQuery

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, upstreamURL.String(), req.Body)
	if err != nil {
		return nil, err
	}

	// Copy headers
	upstreamReq.Header = make(http.Header, len(req.Header))
	for k, vv := range req.Header {
		for _, v := range vv {
			upstreamReq.Header.Add(k, v)
		}
	}

	// If client didn't send Authorization, use configured API key.
	if upstreamReq.Header.Get("Authorization") == "" && a.apiKey != "" {
		upstreamReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	if a.org != "" && upstreamReq.Header.Get("OpenAI-Organization") == "" {
		upstreamReq.Header.Set("OpenAI-Organization", a.org)
	}
	if a.project != "" && upstreamReq.Header.Get("OpenAI-Project") == "" {
		upstreamReq.Header.Set("OpenAI-Project", a.project)
	}

	return a.hc.Do(upstreamReq)
}

func joinURLPath(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	a = strings.TrimRight(a, "/")
	b = strings.TrimLeft(b, "/")
	return a + "/" + b
}

func (a *Client) ParseResponseMeta(body []byte) (providers.ResponseMeta, error) {
	var resp struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return providers.ResponseMeta{}, err
	}

	return providers.ResponseMeta{
		Model:            resp.Model,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
	}, nil
}

func (a *Client) NormalizeError(statusCode int, body []byte) ([]byte, error) {
	var parsed providers.ErrorBody

	// Try to preserve existing OpenAI-style error payload if already compatible.
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		return json.Marshal(parsed)
	}

	parsed.Error.Message = http.StatusText(statusCode)
	parsed.Error.Type = "upstream_error"

	return json.Marshal(parsed)
}
