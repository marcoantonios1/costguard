package metering

import "strings"

func NormalizeProvider(provider string) string {
	switch {
	case strings.HasPrefix(provider, "openai"):
		return "openai"
	case strings.HasPrefix(provider, "anthropic"):
		return "anthropic"
	case strings.HasPrefix(provider, "google"):
		return "google"
	case strings.HasPrefix(provider, "xai"):
		return "xai"
	default:
		return provider
	}
}
