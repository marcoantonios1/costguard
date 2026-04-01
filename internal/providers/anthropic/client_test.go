package anthropic

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

func newAnthropicClient() *Client {
	return &Client{}
}

// ---------------------------------------------------------------------------
// Generic errors
// ---------------------------------------------------------------------------

func TestNormalizeError_Anthropic_AuthError(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	out, err := newAnthropicClient().NormalizeError(http.StatusUnauthorized, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "authentication_error" {
		t.Errorf("type: got %q, want authentication_error", parsed.Error.Type)
	}
	if parsed.Error.Message != "invalid x-api-key" {
		t.Errorf("message: got %q", parsed.Error.Message)
	}
}

func TestNormalizeError_Anthropic_RateLimit(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`)
	out, err := newAnthropicClient().NormalizeError(http.StatusTooManyRequests, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "rate_limit_error" {
		t.Errorf("type: got %q, want rate_limit_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Anthropic_UnparsableBody(t *testing.T) {
	body := []byte(`not json`)
	out, err := newAnthropicClient().NormalizeError(http.StatusBadGateway, body)
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

func TestNormalizeError_Anthropic_InvalidToolName(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"Invalid tool name: must match ^[a-zA-Z0-9_-]{1,64}$"}}`)
	out, err := newAnthropicClient().NormalizeError(http.StatusBadRequest, body)
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

func TestNormalizeError_Anthropic_ToolSchemaValidation(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"tools.0.input_schema: JSON Schema validation failed. 'properties' must be of type object"}}`)
	out, err := newAnthropicClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Anthropic_ToolUseIDNotFound(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"tool_use id toolu_01 not found in tool_use blocks"}}`)
	out, err := newAnthropicClient().NormalizeError(http.StatusBadRequest, body)
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

func TestNormalizeError_Anthropic_MalformedArguments(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"tools[0].input_schema.properties must be an object, got: null"}}`)
	out, err := newAnthropicClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Anthropic_OutputIsValidErrorBodyShape(t *testing.T) {
	// Whatever comes out must always have the {"error":{"message":...}} shape.
	bodies := [][]byte{
		[]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad tool"}}`),
		[]byte(`{"type":"error","error":{"type":"","message":"some error"}}`),
		[]byte(`{}`),
		[]byte(`not json`),
	}
	statuses := []int{400, 400, 500, 502}

	c := newAnthropicClient()
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
