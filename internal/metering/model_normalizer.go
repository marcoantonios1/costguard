package metering

import "strings"

func NormalizeModel(provider, model string) string {
	switch provider {
	case "openai":
		if model == "gpt-4o-mini" || strings.HasPrefix(model, "gpt-4o-mini-") {
			return "gpt-4o-mini"
		}
		if model == "gpt-4o" || strings.HasPrefix(model, "gpt-4o-") {
			return "gpt-4o"
		}
		if model == "gpt-4.1-nano" || strings.HasPrefix(model, "gpt-4.1-nano-") {
			return "gpt-4.1-nano"
		}
		if model == "gpt-4.1-mini" || strings.HasPrefix(model, "gpt-4.1-mini-") {
			return "gpt-4.1-mini"
		}
		if model == "gpt-4.1" || strings.HasPrefix(model, "gpt-4.1-") {
			return "gpt-4.1"
		}
		if model == "gpt-5.4" || strings.HasPrefix(model, "gpt-5.4-") {
			return "gpt-5.4"
		}
		if model == "gpt-5.4-mini" || strings.HasPrefix(model, "gpt-5.4-mini-") {
			return "gpt-5.4-mini"
		}
		if model == "gpt-5.4-nano" || strings.HasPrefix(model, "gpt-5.4-nano-") {
			return "gpt-5.4-nano"
		}

	case "anthropic":
		if model == "claude-sonnet-4-6" || strings.HasPrefix(model, "claude-sonnet-4-6-") {
			return "claude-sonnet-4-6"
		}
		if model == "claude-opus-4-6" || strings.HasPrefix(model, "claude-opus-4-6-") {
			return "claude-opus-4-6"
		}
		if model == "claude-sonnet-4-5" || strings.HasPrefix(model, "claude-sonnet-4-5-") {
			return "claude-sonnet-4-5"
		}
		if model == "claude-haiku-4-5" || strings.HasPrefix(model, "claude-haiku-4-5-") {
			return "claude-haiku-4-5"
		}

	case "google":
		if model == "gemini-2.5-flash-lite" || strings.HasPrefix(model, "gemini-2.5-flash-lite-") {
			return "gemini-2.5-flash-lite"
		}
		if model == "gemini-2.5-flash" || strings.HasPrefix(model, "gemini-2.5-flash-") {
			return "gemini-2.5-flash"
		}
		if model == "gemini-2.5-pro" || strings.HasPrefix(model, "gemini-2.5-pro-") {
			return "gemini-2.5-pro"
		}
	}

	return model
}
