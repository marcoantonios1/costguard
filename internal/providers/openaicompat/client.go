package openaicompat

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

	"github.com/marcoantonios1/costguard/internal/providers"
)

type ClientConfig struct {
	Name            string
	BaseURL         string
	APIKey          string
	Timeout         time.Duration
	AllowMultimodal bool
}

type Client struct {
	name            string
	base            *url.URL
	apiKey          string
	hc              *http.Client
	allowMultimodal bool
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("openai-compatible client name is required")
	}

	base := cfg.BaseURL
	if base == "" {
		return nil, fmt.Errorf("openai-compatible base_url is required")
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
		name:            cfg.Name,
		base:            u,
		apiKey:          cfg.APIKey,
		hc:              &http.Client{Timeout: to},
		allowMultimodal: cfg.AllowMultimodal,
	}, nil
}

func (a *Client) Name() string { return a.name }

func (a *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("nil request")
	}

	if !a.allowMultimodal && req.Method == http.MethodPost && req.URL.Path == "/v1/chat/completions" {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(body))

		if requestHasImageContent(body) {
			return multimodalNotAllowedResponse(req), nil
		}
	}

	upstreamURL := *a.base
	upstreamURL.Path = joinURLPath(a.base.Path, req.URL.Path)
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

	if upstreamReq.Header.Get("Authorization") == "" && strings.TrimSpace(a.apiKey) != "" {
		upstreamReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	}

	return a.hc.Do(upstreamReq)
}

func requestHasImageContent(body []byte) bool {
	var req struct {
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return false
	}
	for _, msg := range req.Messages {
		blocks, ok := msg.Content.([]any)
		if !ok {
			continue
		}
		for _, b := range blocks {
			m, ok := b.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] == "image_url" {
				return true
			}
		}
	}
	return false
}

func multimodalNotAllowedResponse(req *http.Request) *http.Response {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"message": "this model may not support vision; set allow_multimodal: true on the provider config to enable image passthrough",
			"type":    "invalid_request_error",
		},
	})
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Status:     "400 Bad Request",
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
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

	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		return json.Marshal(parsed)
	}

	parsed.Error.Message = http.StatusText(statusCode)
	parsed.Error.Type = "upstream_error"

	return json.Marshal(parsed)
}
