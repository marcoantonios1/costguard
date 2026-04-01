package gemini

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

func newGeminiClient() *Client {
	return &Client{}
}

// ---------------------------------------------------------------------------
// Generic errors
// ---------------------------------------------------------------------------

func TestNormalizeError_Gemini_AuthError(t *testing.T) {
	body := []byte(`{"error":{"code":401,"message":"API key not valid. Please pass a valid API key.","status":"UNAUTHENTICATED"}}`)
	out, err := newGeminiClient().NormalizeError(http.StatusUnauthorized, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "authentication_error" {
		t.Errorf("type: got %q, want authentication_error", parsed.Error.Type)
	}
	if parsed.Error.Code != "UNAUTHENTICATED" {
		t.Errorf("code: got %q, want UNAUTHENTICATED", parsed.Error.Code)
	}
}

func TestNormalizeError_Gemini_RateLimit(t *testing.T) {
	body := []byte(`{"error":{"code":429,"message":"Resource has been exhausted (e.g. check quota).","status":"RESOURCE_EXHAUSTED"}}`)
	out, err := newGeminiClient().NormalizeError(http.StatusTooManyRequests, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "rate_limit_error" {
		t.Errorf("type: got %q, want rate_limit_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Gemini_PermissionDenied(t *testing.T) {
	body := []byte(`{"error":{"code":403,"message":"The caller does not have permission.","status":"PERMISSION_DENIED"}}`)
	out, err := newGeminiClient().NormalizeError(http.StatusForbidden, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "permission_error" {
		t.Errorf("type: got %q, want permission_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Gemini_InternalError(t *testing.T) {
	body := []byte(`{"error":{"code":500,"message":"Internal error encountered.","status":"INTERNAL"}}`)
	out, err := newGeminiClient().NormalizeError(http.StatusInternalServerError, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "api_error" {
		t.Errorf("type: got %q, want api_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Gemini_UnparsableBody(t *testing.T) {
	body := []byte(`not json`)
	out, err := newGeminiClient().NormalizeError(http.StatusBadGateway, body)
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

func TestNormalizeError_Gemini_UnknownStatus(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"Something weird happened.","status":"UNKNOWN_STATUS"}}`)
	out, err := newGeminiClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "upstream_error" {
		t.Errorf("type: got %q, want upstream_error", parsed.Error.Type)
	}
}

// ---------------------------------------------------------------------------
// Tool-related errors
// ---------------------------------------------------------------------------

func TestNormalizeError_Gemini_InvalidFunctionName(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"Invalid function name: my-func. Function name must match ^[a-zA-Z_][a-zA-Z0-9_]*$","status":"INVALID_ARGUMENT"}}`)
	out, err := newGeminiClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
	if parsed.Error.Code != "INVALID_ARGUMENT" {
		t.Errorf("code: got %q, want INVALID_ARGUMENT", parsed.Error.Code)
	}
	if parsed.Error.Message == "" {
		t.Error("expected a non-empty message")
	}
}

func TestNormalizeError_Gemini_MalformedFunctionDeclaration(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"Function declaration get_weather is not valid. Schema type ARRAY cannot have properties field.","status":"INVALID_ARGUMENT"}}`)
	out, err := newGeminiClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
}

func TestNormalizeError_Gemini_FunctionResponseMismatch(t *testing.T) {
	body := []byte(`{"error":{"code":400,"message":"The model does not support function response messages in this context.","status":"INVALID_ARGUMENT"}}`)
	out, err := newGeminiClient().NormalizeError(http.StatusBadRequest, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parsed := parseErrorBody(t, out)
	if parsed.Error.Type != "invalid_request_error" {
		t.Errorf("type: got %q, want invalid_request_error", parsed.Error.Type)
	}
}

// ---------------------------------------------------------------------------
// geminiStatusToErrorType unit tests
// ---------------------------------------------------------------------------

func TestGeminiStatusToErrorType(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"INVALID_ARGUMENT", "invalid_request_error"},
		{"PERMISSION_DENIED", "permission_error"},
		{"UNAUTHENTICATED", "authentication_error"},
		{"RESOURCE_EXHAUSTED", "rate_limit_error"},
		{"NOT_FOUND", "not_found_error"},
		{"INTERNAL", "api_error"},
		{"UNAVAILABLE", "api_error"},
		{"", "upstream_error"},
		{"SOME_FUTURE_STATUS", "upstream_error"},
	}
	for _, tc := range cases {
		got := geminiStatusToErrorType(tc.status)
		if got != tc.want {
			t.Errorf("geminiStatusToErrorType(%q): got %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestNormalizeError_Gemini_OutputIsValidErrorBodyShape(t *testing.T) {
	bodies := [][]byte{
		[]byte(`{"error":{"code":400,"message":"Invalid function name","status":"INVALID_ARGUMENT"}}`),
		[]byte(`{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`),
		[]byte(`{}`),
		[]byte(`not json`),
	}
	statuses := []int{400, 429, 500, 502}

	c := newGeminiClient()
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
