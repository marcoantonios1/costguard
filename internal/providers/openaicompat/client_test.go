package openaicompat

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

func newCompatClient() *Client {
	return &Client{}
}

// ---------------------------------------------------------------------------
// Generic errors
// ---------------------------------------------------------------------------

func TestNormalizeError_Compat_PassthroughOpenAIShape(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid API key.","type":"authentication_error"}}`)
	out, err := newCompatClient().NormalizeError(http.StatusUnauthorized, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "authentication_error" {
		t.Errorf("type: got %q, want authentication_error", parsed.Error.Type)
	}
	if parsed.Error.Message != "Invalid API key." {
		t.Errorf("message: got %q", parsed.Error.Message)
	}
}

func TestNormalizeError_Compat_UnparsableBody(t *testing.T) {
	body := []byte(`not json`)
	out, err := newCompatClient().NormalizeError(http.StatusBadGateway, body)
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

func TestNormalizeError_Compat_EmptyErrorMessage(t *testing.T) {
	// Body has the right shape but empty message — fall through to status text.
	body := []byte(`{"error":{"message":"","type":"some_error"}}`)
	out, err := newCompatClient().NormalizeError(http.StatusInternalServerError, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Message == "" {
		t.Error("expected a non-empty fallback message")
	}
}

// ---------------------------------------------------------------------------
// Tool-related errors
// ---------------------------------------------------------------------------

func TestNormalizeError_Compat_InvalidToolName(t *testing.T) {
	body := []byte(`{"error":{"message":"Invalid tool name: must not contain hyphens.","type":"invalid_request_error"}}`)
	out, err := newCompatClient().NormalizeError(http.StatusBadRequest, body)
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

func TestNormalizeError_Compat_MalformedToolSchema(t *testing.T) {
	body := []byte(`{"error":{"message":"tools[0].function.parameters must be a valid JSON Schema object.","type":"invalid_request_error"}}`)
	out, err := newCompatClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Compat_OutputIsValidErrorBodyShape(t *testing.T) {
	bodies := [][]byte{
		[]byte(`{"error":{"message":"Invalid tool name","type":"invalid_request_error"}}`),
		[]byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`),
		[]byte(`{}`),
		[]byte(`not json`),
	}
	statuses := []int{400, 429, 500, 502}

	c := newCompatClient()
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
