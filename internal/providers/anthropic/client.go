package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ClientConfig struct {
	Name             string
	BaseURL          string
	APIKey           string
	AnthropicVersion string
	Timeout          time.Duration
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

	switch req.URL.Path {
	case "/v1/chat/completions":
		return c.doChatCompletions(ctx, req)
	default:
		return jsonResponse(
			req,
			http.StatusNotImplemented,
			map[string]any{
				"error": map[string]any{
					"message": fmt.Sprintf("anthropic adapter does not support path %s yet", req.URL.Path),
					"type":    "not_implemented",
				},
			},
		)
	}
}

func (c *Client) doChatCompletions(ctx context.Context, req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	var oaReq openAIChatCompletionRequest
	if err := json.Unmarshal(body, &oaReq); err != nil {
		return jsonResponse(
			req,
			http.StatusBadRequest,
			map[string]any{
				"error": map[string]any{
					"message": "invalid OpenAI chat completion request body",
					"type":    "invalid_request_error",
				},
			},
		)
	}

	anthReq, err := toAnthropicRequest(oaReq)
	if err != nil {
		return jsonResponse(
			req,
			http.StatusBadRequest,
			map[string]any{
				"error": map[string]any{
					"message": err.Error(),
					"type":    "invalid_request_error",
				},
			},
		)
	}

	payload, err := json.Marshal(anthReq)
	if err != nil {
		return nil, err
	}

	upstreamURL := *c.base
	upstreamURL.Path = joinURLPath(c.base.Path, "/v1/messages")

	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	upstreamReq.Header = make(http.Header)
	copyAllowedHeaders(req.Header, upstreamReq.Header)

	upstreamReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		upstreamReq.Header.Set("x-api-key", c.apiKey)
	}
	if c.anthropicVersion != "" {
		upstreamReq.Header.Set("anthropic-version", c.anthropicVersion)
	}

	upstreamResp, err := c.hc.Do(upstreamReq)
	if err != nil {
		return nil, err
	}
	defer upstreamResp.Body.Close()

	respBody, err := io.ReadAll(upstreamResp.Body)
	if err != nil {
		return nil, err
	}

	if upstreamResp.StatusCode < 200 || upstreamResp.StatusCode >= 300 {
		return rawJSONResponse(req, upstreamResp.StatusCode, upstreamResp.Header, respBody), nil
	}

	var anthResp anthropicMessagesResponse
	if err := json.Unmarshal(respBody, &anthResp); err != nil {
		return nil, fmt.Errorf("failed to parse anthropic response: %w", err)
	}

	normalized := toOpenAIResponse(oaReq.Model, anthResp)

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	if orgID := upstreamResp.Header.Get("anthropic-organization-id"); orgID != "" {
		header.Set("anthropic-organization-id", orgID)
	}

	return jsonResponseWithHeader(req, http.StatusOK, header, normalized)
}

func copyAllowedHeaders(src, dst http.Header) {
	for k, vv := range src {
		switch http.CanonicalHeaderKey(k) {
		case "X-Request-Id", "X-Costguard-Team", "X-Costguard-Project", "X-Costguard-User":
			for _, v := range vv {
				dst.Add(k, v)
			}
		}
	}
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

func jsonResponse(req *http.Request, status int, payload any) (*http.Response, error) {
	return jsonResponseWithHeader(req, status, http.Header{
		"Content-Type": []string{"application/json"},
	}, payload)
}

func jsonResponseWithHeader(req *http.Request, status int, header http.Header, payload any) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}, nil
}

func rawJSONResponse(req *http.Request, status int, srcHeader http.Header, body []byte) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	if v := srcHeader.Get("anthropic-organization-id"); v != "" {
		header.Set("anthropic-organization-id", v)
	}

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}