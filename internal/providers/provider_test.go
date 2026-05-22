package providers

import "testing"

func TestErrorCategory_TypeTakesPrecedence(t *testing.T) {
	// type wins even when status code would give a different answer
	cases := []struct {
		errType    string
		statusCode int
		want       string
	}{
		{"authentication_error", 200, ErrCategoryAuth},
		{"permission_error", 200, ErrCategoryAuth},
		{"invalid_request_error", 200, ErrCategoryInvalidRequest},
		{"rate_limit_error", 200, ErrCategoryRateLimit},
	}
	for _, tc := range cases {
		got := ErrorCategory(tc.errType, tc.statusCode)
		if got != tc.want {
			t.Errorf("ErrorCategory(%q, %d) = %q, want %q", tc.errType, tc.statusCode, got, tc.want)
		}
	}
}

func TestErrorCategory_StatusFallback(t *testing.T) {
	cases := []struct {
		statusCode int
		want       string
	}{
		{401, ErrCategoryAuth},
		{403, ErrCategoryAuth},
		{400, ErrCategoryInvalidRequest},
		{422, ErrCategoryInvalidRequest},
		{429, ErrCategoryRateLimit},
		{502, ErrCategoryProviderUnavailable},
		{503, ErrCategoryProviderUnavailable},
		{504, ErrCategoryProviderUnavailable},
		{500, ErrCategoryUpstreamFailure},
		{0, ErrCategoryUpstreamFailure},
	}
	for _, tc := range cases {
		got := ErrorCategory("", tc.statusCode)
		if got != tc.want {
			t.Errorf("ErrorCategory(\"\", %d) = %q, want %q", tc.statusCode, got, tc.want)
		}
	}
}

func TestErrorCategory_KnownValues(t *testing.T) {
	if ErrorCategory("authentication_error", 401) != ErrCategoryAuth {
		t.Error("authentication_error/401 should be auth")
	}
	if ErrorCategory("rate_limit_error", 429) != ErrCategoryRateLimit {
		t.Error("rate_limit_error/429 should be rate_limit")
	}
	if ErrorCategory("", 503) != ErrCategoryProviderUnavailable {
		t.Error("\"\"/503 should be provider_unavailable")
	}
	if ErrorCategory("", 500) != ErrCategoryUpstreamFailure {
		t.Error("\"\"/500 should be upstream_failure")
	}
}
