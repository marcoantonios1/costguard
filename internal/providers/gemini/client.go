package gemini

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
	Name    string
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

type Client struct {
	name     string
	base     *url.URL
	apiKey   string
	hc       *http.Client
	streamHC *http.Client // no timeout — streaming duration is unbounded
}

func NewClient(cfg ClientConfig) (*Client, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("gemini client name is required")
	}

	base := cfg.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
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
		name:     cfg.Name,
		base:     u,
		apiKey:   cfg.APIKey,
		hc:       &http.Client{Timeout: to},
		streamHC: &http.Client{}, // no timeout; context handles cancellation
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
					"message": fmt.Sprintf("gemini adapter does not support path %s yet", req.URL.Path),
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

	fetch := func(u string) ([]byte, string, error) {
		resp, err := c.hc.Get(u)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, "", fmt.Errorf("HTTP %d fetching image", resp.StatusCode)
		}
		ct := resp.Header.Get("Content-Type")
		if idx := strings.IndexByte(ct, ';'); idx >= 0 {
			ct = strings.TrimSpace(ct[:idx])
		}
		data, err := io.ReadAll(resp.Body)
		return data, ct, err
	}

	gemReq, err := toGeminiRequest(oaReq, fetch)
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

	if oaReq.Stream {
		return c.doStreamingChatCompletions(ctx, req, oaReq, gemReq)
	}

	payload, err := json.Marshal(gemReq)
	if err != nil {
		return nil, err
	}

	upstreamURL := *c.base
	upstreamURL.Path = joinURLPath(c.base.Path, fmt.Sprintf("/v1beta/models/%s:generateContent", oaReq.Model))
	q := upstreamURL.Query()
	if c.apiKey != "" {
		q.Set("key", c.apiKey)
	}
	upstreamURL.RawQuery = q.Encode()

	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	upstreamReq.Header = make(http.Header)
	copyAllowedHeaders(req.Header, upstreamReq.Header)
	upstreamReq.Header.Set("Content-Type", "application/json")

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
		return rawJSONResponse(req, upstreamResp.StatusCode, respBody), nil
	}

	var gemResp geminiGenerateContentResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return nil, fmt.Errorf("failed to parse gemini response: %w", err)
	}

	normalized := toOpenAIResponse(oaReq.Model, gemResp)

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return jsonResponseWithHeader(req, http.StatusOK, header, normalized)
}

func (c *Client) doStreamingChatCompletions(ctx context.Context, req *http.Request, oaReq openAIChatCompletionRequest, gemReq geminiGenerateContentRequest) (*http.Response, error) {
	payload, err := json.Marshal(gemReq)
	if err != nil {
		return nil, err
	}

	upstreamURL := *c.base
	upstreamURL.Path = joinURLPath(c.base.Path, fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", oaReq.Model))
	q := upstreamURL.Query()
	q.Set("alt", "sse")
	if c.apiKey != "" {
		q.Set("key", c.apiKey)
	}
	upstreamURL.RawQuery = q.Encode()

	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}

	upstreamReq.Header = make(http.Header)
	copyAllowedHeaders(req.Header, upstreamReq.Header)
	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Accept", "text/event-stream")

	upstreamResp, err := c.streamHC.Do(upstreamReq)
	if err != nil {
		return nil, err
	}

	if upstreamResp.StatusCode < 200 || upstreamResp.StatusCode >= 300 {
		defer upstreamResp.Body.Close()
		respBody, _ := io.ReadAll(upstreamResp.Body)
		return rawJSONResponse(req, upstreamResp.StatusCode, respBody), nil
	}

	pr, pw := io.Pipe()
	go func() {
		defer upstreamResp.Body.Close()
		defer pw.Close()
		translateGeminiStream(oaReq.Model, upstreamResp.Body, pw)
	}()

	header := make(http.Header)
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     header,
		Body:       pr,
		Request:    req,
	}, nil
}

func copyAllowedHeaders(src, dst http.Header) {
	for k, vv := range src {
		switch http.CanonicalHeaderKey(k) {
		case "X-Request-Id", "X-Costguard-Team", "X-Costguard-Project", "X-Costguard-User", "X-Costguard-Agent":
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

func rawJSONResponse(req *http.Request, status int, body []byte) *http.Response {
	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func (c *Client) ParseResponseMeta(body []byte) (providers.ResponseMeta, error) {
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

func (c *Client) NormalizeError(statusCode int, body []byte) ([]byte, error) {
	var raw struct {
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}

	var out providers.ErrorBody

	if err := json.Unmarshal(body, &raw); err == nil && raw.Error.Message != "" {
		out.Error.Message = raw.Error.Message
		out.Error.Type = geminiStatusToErrorType(raw.Error.Status)
		out.Error.Code = raw.Error.Status
		return json.Marshal(out)
	}

	out.Error.Message = http.StatusText(statusCode)
	out.Error.Type = "upstream_error"
	return json.Marshal(out)
}

// geminiStatusToErrorType maps Gemini gRPC status strings to OpenAI error type names.
func geminiStatusToErrorType(status string) string {
	switch status {
	case "INVALID_ARGUMENT":
		return "invalid_request_error"
	case "PERMISSION_DENIED":
		return "permission_error"
	case "UNAUTHENTICATED":
		return "authentication_error"
	case "RESOURCE_EXHAUSTED":
		return "rate_limit_error"
	case "NOT_FOUND":
		return "not_found_error"
	case "INTERNAL", "UNAVAILABLE":
		return "api_error"
	default:
		return "upstream_error"
	}
}
