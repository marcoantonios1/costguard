package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ClientConfig struct {
	Name              string
	BaseURL           string
	APIKey            string
	AnthropicVersion  string
	Timeout           time.Duration
}

type Client struct {
	name             string
	base             *url.URL
	apiKey           string
	anthropicVersion string
	hc               *http.Client
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("anthropic client name is required")
	}

	base := cfg.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}

	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}

	to := cfg.Timeout
	if to == 0 {
		to = 60 * time.Second
	}

	version := cfg.AnthropicVersion
	if version == "" {
		version = "2023-06-01"
	}

	return &Client{
		name:             cfg.Name,
		base:             u,
		apiKey:           cfg.APIKey,
		anthropicVersion: version,
		hc:               &http.Client{Timeout: to},
	}, nil
}

func (c *Client) Name() string { return c.name }

func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}

	upstreamURL := *c.base
	upstreamURL.Path = joinURLPath(c.base.Path, req.URL.Path)
	upstreamURL.RawQuery = req.URL.RawQuery

	upstreamReq, err := http.NewRequestWithContext(ctx, req.Method, upstreamURL.String(), req.Body)
	if err != nil {
		return nil, err
	}

	upstreamReq.Header = make(http.Header, len(req.Header))
	for k, vv := range req.Header {
		for _, v := range vv {
			upstreamReq.Header.Add(k, v)
		}
	}

	if upstreamReq.Header.Get("x-api-key") == "" && c.apiKey != "" {
		upstreamReq.Header.Set("x-api-key", c.apiKey)
	}

	if upstreamReq.Header.Get("anthropic-version") == "" && c.anthropicVersion != "" {
		upstreamReq.Header.Set("anthropic-version", c.anthropicVersion)
	}

	return c.hc.Do(upstreamReq)
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