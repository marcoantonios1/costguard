package openai

import (
	"encoding/json"
	"net/http"
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
