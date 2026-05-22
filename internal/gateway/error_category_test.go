package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/marcoantonios1/costguard/internal/providers"
)

func TestGatewayErrorCategoryAndType(t *testing.T) {
	cases := []struct {
		name         string
		err          error
		wantCategory string
		wantType     string
	}{
		{
			name:         "nil",
			err:          nil,
			wantCategory: providers.ErrCategoryUpstreamFailure,
			wantType:     "upstream_error",
		},
		{
			name:         "context_deadline_exceeded",
			err:          context.DeadlineExceeded,
			wantCategory: providers.ErrCategoryProviderUnavailable,
			wantType:     "provider_unavailable_error",
		},
		{
			name:         "provider_timeout_wrapped",
			err:          errors.New("provider_timeout provider=anthropic_primary: context deadline exceeded"),
			wantCategory: providers.ErrCategoryProviderUnavailable,
			wantType:     "provider_unavailable_error",
		},
		{
			name:         "upstream_5xx",
			err:          errors.New("upstream_5xx status=503 provider=openai_primary"),
			wantCategory: providers.ErrCategoryUpstreamFailure,
			wantType:     "upstream_error",
		},
		{
			name:         "connection_refused",
			err:          errors.New("dial tcp [::1]:9000: connect: connection refused"),
			wantCategory: providers.ErrCategoryProviderUnavailable,
			wantType:     "provider_unavailable_error",
		},
		{
			name:         "unknown_error",
			err:          errors.New("something unexpected"),
			wantCategory: providers.ErrCategoryUpstreamFailure,
			wantType:     "upstream_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotCategory, gotType := gatewayErrorCategoryAndType(tc.err)
			if gotCategory != tc.wantCategory {
				t.Errorf("category: got %q, want %q", gotCategory, tc.wantCategory)
			}
			if gotType != tc.wantType {
				t.Errorf("type: got %q, want %q", gotType, tc.wantType)
			}
		})
	}
}
