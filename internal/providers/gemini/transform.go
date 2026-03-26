package gemini

import (
	"fmt"
	"strings"
	"time"
)

func toGeminiRequest(in openAIChatCompletionRequest) (geminiGenerateContentRequest, error) {
	if in.Model == "" {
		return geminiGenerateContentRequest{}, fmt.Errorf("model is required")
	}
	if len(in.Messages) == 0 {
		return geminiGenerateContentRequest{}, fmt.Errorf("messages are required")
	}

	var systemTexts []string
	var contents []geminiContent

	for _, msg := range in.Messages {
		text, err := contentToText(msg.Content)
		if err != nil {
			return geminiGenerateContentRequest{}, err
		}
		if strings.TrimSpace(text) == "" {
			continue
		}

		switch msg.Role {
		case "system":
			systemTexts = append(systemTexts, text)

		case "user":
			contents = append(contents, geminiContent{
				Role: "user",
				Parts: []geminiPart{
					{Text: text},
				},
			})

		case "assistant":
			contents = append(contents, geminiContent{
				Role: "model",
				Parts: []geminiPart{
					{Text: text},
				},
			})

		default:
			return geminiGenerateContentRequest{}, fmt.Errorf("unsupported message role: %s", msg.Role)
		}
	}

	if len(contents) == 0 {
		return geminiGenerateContentRequest{}, fmt.Errorf("at least one non-system message is required")
	}

	out := geminiGenerateContentRequest{
		Contents: contents,
	}

	if len(systemTexts) > 0 {
		out.SystemInstruction = &geminiSystemInstruction{
			Parts: []geminiPart{
				{Text: strings.Join(systemTexts, "\n\n")},
			},
		}
	}

	cfg := &geminiGenerationConfig{}
	hasCfg := false

	if in.Temperature != nil {
		cfg.Temperature = in.Temperature
		hasCfg = true
	}
	if in.TopP != nil {
		cfg.TopP = in.TopP
		hasCfg = true
	}
	if in.MaxTokens > 0 {
		cfg.MaxOutputTokens = in.MaxTokens
		hasCfg = true
	}

	switch v := in.Stop.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			cfg.StopSequences = []string{v}
			hasCfg = true
		}
	case []string:
		if len(v) > 0 {
			cfg.StopSequences = v
			hasCfg = true
		}
	case nil:
	default:
		return geminiGenerateContentRequest{}, fmt.Errorf("unsupported stop format")
	}

	if hasCfg {
		out.GenerationConfig = cfg
	}

	return out, nil
}

func toOpenAIResponse(requestModel string, in geminiGenerateContentResponse) openAIChatCompletionResponse {
	content := ""
	finishReason := "stop"

	if len(in.Candidates) > 0 {
		c := in.Candidates[0]
		finishReason = normalizeFinishReason(c.FinishReason)
		content = extractGeminiText(c.Content)
	}

	return openAIChatCompletionResponse{
		ID:      fallbackString(in.ResponseID, fmt.Sprintf("chatcmpl-gemini-%d", time.Now().UnixNano())),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   fallbackString(in.ModelVersion, requestModel),
		Choices: []openAIChoice{
			{
				Index: 0,
				Message: openAIAssistantMsg{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: finishReason,
			},
		},
		Usage: openAIUsage{
			PromptTokens:     in.UsageMetadata.PromptTokenCount,
			CompletionTokens: in.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      in.UsageMetadata.TotalTokenCount,
		},
	}
}

func extractGeminiText(content geminiContent) string {
	var parts []string
	for _, p := range content.Parts {
		if strings.TrimSpace(p.Text) != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func normalizeFinishReason(v string) string {
	switch strings.ToUpper(v) {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "content_filter"
	default:
		return "stop"
	}
}

func fallbackString(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func contentToText(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil

	case []any:
		var parts []string
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			if typ == "text" {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n"), nil

	case nil:
		return "", nil

	default:
		return "", fmt.Errorf("unsupported message content format")
	}
}
