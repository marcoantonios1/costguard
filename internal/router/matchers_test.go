package router_test

import "testing"

func TestMatchProviderByModel(t *testing.T) {
	cases := []struct {
		model string
		want  string
	}{
		{"gpt-4o", "openai_primary"},
		{"gpt-4o-mini", "openai_primary"},
		{"gpt-4.1", "openai_primary"},
		{"claude-sonnet-4-6", "anthropic_primary"},
		{"claude-opus-4-6", "anthropic_primary"},
		{"gemini-2.5-flash", "google_primary"},
		{"gemini-2.5-pro", "google_primary"},
		{"gemini-2.5-flash-lite", "google_primary"},
		{"llama3.2", ""},
		{"medgemma:27b", ""},
		{"unknown-model", ""},
		{"", ""},
	}

	for _, tc := range cases {
		got := MatchProviderByModel(tc.model)
		if got != tc.want {
			t.Errorf("MatchProviderByModel(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}
