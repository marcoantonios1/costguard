package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/marcoantonios1/costguard/internal/providers"
)

func parseErrorBody(t *testing.T, b []byte) providers.ErrorBody {
	t.Helper()
	var out providers.ErrorBody
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("failed to parse normalized error: %v\nbody: %s", err, b)
	}
	return out
}

func newOpenAIClient() *Client {
	return &Client{}
}

// ---------------------------------------------------------------------------
// Generic errors
// ---------------------------------------------------------------------------

func TestNormalizeError_OpenAI_AuthError(t *testing.T) {
	body := []byte(`{"error":{"message":"Incorrect API key provided.","type":"invalid_api_key","code":"invalid_api_key"}}`)
	out, err := newOpenAIClient().NormalizeError(http.StatusUnauthorized, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_api_key" {
		t.Errorf("type: got %q, want invalid_api_key", parsed.Error.Type)
	}
	if parsed.Error.Message != "Incorrect API key provided." {
		t.Errorf("message: got %q", parsed.Error.Message)
	}
}

func TestNormalizeError_OpenAI_RateLimit(t *testing.T) {
	body := []byte(`{"error":{"message":"Rate limit reached for model gpt-4o.","type":"requests","code":"rate_limit_exceeded"}}`)
	out, err := newOpenAIClient().NormalizeError(http.StatusTooManyRequests, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Message == "" {
		t.Error("expected a non-empty message")
	}
}

func TestNormalizeError_OpenAI_UnparsableBody(t *testing.T) {
	body := []byte(`not json`)
	out, err := newOpenAIClient().NormalizeError(http.StatusBadGateway, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "upstream_error" {
		t.Errorf("type: got %q, want upstream_error", parsed.Error.Type)
	}
	if parsed.Error.Message == "" {
		t.Error("expected a non-empty fallback message")
	}
}

// ---------------------------------------------------------------------------
// Tool-related errors
// ---------------------------------------------------------------------------

func TestNormalizeError_OpenAI_InvalidFunctionArguments(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid 'tools[0].function.name': string does not match pattern.","type":"invalid_request_error","code":"invalid_function_name"}}`)
	out, err := newOpenAIClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
	if parsed.Error.Message == "" {
		t.Error("expected a non-empty message")
	}
}

func TestNormalizeError_OpenAI_MalformedToolSchema(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid schema for function 'get_weather': schema must be a JSON Schema object.","type":"invalid_request_error","code":null}}`)
	out, err := newOpenAIClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
}

// ---------------------------------------------------------------------------
// Do — image content passthrough
// ---------------------------------------------------------------------------

// minimalOKResponse is a valid OpenAI chat completion JSON returned by the
// stub server so ParseResponseMeta and metering don't choke.
const minimalOKResponse = `{"id":"x","object":"chat.completion","created":1,"model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`

func stubServer(t *testing.T, capture *[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*capture = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(minimalOKResponse))
	}))
}

func makeRequest(t *testing.T, srvURL, payload string) *http.Request {
	t.Helper()
	u, _ := url.Parse(srvURL + "/v1/chat/completions")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, u.String(), strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestDo_ImageURLBlockForwardedUnchanged(t *testing.T) {
	var received []byte
	srv := stubServer(t, &received)
	defer srv.Close()

	client, _ := NewClient(ClientConfig{Name: "test", BaseURL: srv.URL})

	payload := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/photo.png"}}]}]}`
	resp, err := client.Do(context.Background(), makeRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if string(received) != payload {
		t.Errorf("body modified in transit\ngot:  %s\nwant: %s", received, payload)
	}
}

func TestDo_DetailFieldPreserved(t *testing.T) {
	var received []byte
	srv := stubServer(t, &received)
	defer srv.Close()

	client, _ := NewClient(ClientConfig{Name: "test", BaseURL: srv.URL})

	payload := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/photo.png","detail":"high"}}]}]}`
	resp, err := client.Do(context.Background(), makeRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.Unmarshal(received, &body); err != nil {
		t.Fatalf("received body is not valid JSON: %v", err)
	}
	messages := body["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	imageURL := content[0].(map[string]any)["image_url"].(map[string]any)
	if imageURL["detail"] != "high" {
		t.Errorf("detail field lost: got %v", imageURL["detail"])
	}
}

func TestDo_ImageOnlyMessageForwardedWithoutError(t *testing.T) {
	var received []byte
	srv := stubServer(t, &received)
	defer srv.Close()

	client, _ := NewClient(ClientConfig{Name: "test", BaseURL: srv.URL})

	payload := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/photo.png"}}]}]}`
	resp, err := client.Do(context.Background(), makeRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
	if len(received) == 0 {
		t.Error("upstream received empty body")
	}
}

func TestDo_MixedTextAndImageBlocksForwardedUnchanged(t *testing.T) {
	var received []byte
	srv := stubServer(t, &received)
	defer srv.Close()

	client, _ := NewClient(ClientConfig{Name: "test", BaseURL: srv.URL})

	payload := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"low"}},{"type":"image_url","image_url":{"url":"https://example.com/b.png"}}]}]}`
	resp, err := client.Do(context.Background(), makeRequest(t, srv.URL, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if string(received) != payload {
		t.Errorf("body modified in transit\ngot:  %s\nwant: %s", received, payload)
	}
}

func TestNormalizeError_OpenAI_OutputIsValidErrorBodyShape(t *testing.T) {
	bodies := [][]byte{
		[]byte(`{"error":{"message":"Invalid function name","type":"invalid_request_error"}}`),
		[]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`),
		[]byte(`{}`),
		[]byte(`not json`),
	}
	statuses := []int{400, 429, 500, 502}

	c := newOpenAIClient()
	for i, body := range bodies {
		out, err := c.NormalizeError(statuses[i], body)
		if err != nil {
			t.Errorf("body[%d]: unexpected error: %v", i, err)
			continue
		}
		var shape struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(out, &shape); err != nil {
			t.Errorf("body[%d]: output is not valid JSON: %v", i, err)
			continue
		}
		if shape.Error.Message == "" {
			t.Errorf("body[%d]: error.message is empty", i)
		}
	}
}
