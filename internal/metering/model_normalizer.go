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
	}

	return model
}
