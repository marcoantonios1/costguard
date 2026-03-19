package router

import "strings"

func MatchProviderByModel(model string) string {
	model = strings.TrimSpace(strings.ToLower(model))

	switch {
	case strings.HasPrefix(model, "gpt-"):
		return "openai_primary"
	case strings.HasPrefix(model, "claude-"):
		return "anthropic_primary"
	default:
		return ""
	}
}