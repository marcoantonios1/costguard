package anthropic

import (
	"fmt"
	"strings"
	"time"
)

func toAnthropicRequest(in openAIChatCompletionRequest) (anthropicMessagesRequest, error) {
	if in.Model == "" {
		return anthropicMessagesRequest{}, fmt.Errorf("model is required")
	}
	if len(in.Messages) == 0 {
		return anthropicMessagesRequest{}, fmt.Errorf("messages is required")
	}
	if in.Stream {
		return anthropicMessagesRequest{}, fmt.Errorf("stream is not supported by the anthropic adapter yet")
	}

	out := anthropicMessagesRequest{
		Model:     in.Model,
		MaxTokens: in.MaxTokens,
		Stream:    false,
	}

	if out.MaxTokens <= 0 {
		out.MaxTokens = 1024
	}

	if in.Temperature != nil {
		out.Temperature = in.Temperature
	}
	if in.TopP != nil {
		out.TopP = in.TopP
	}

	switch v := in.Stop.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			out.StopSeqs = []string{v}
		}
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if ok && strings.TrimSpace(s) != "" {
				out.StopSeqs = append(out.StopSeqs, s)
			}
		}
	case []string:
		out.StopSeqs = append(out.StopSeqs, v...)
	}

	for _, msg := range in.Messages {
		text, err := openAIContentToText(msg.Content)
		if err != nil {
			return anthropicMessagesRequest{}, err
		}

		switch msg.Role {
		case "system":
			if out.System == "" {
				out.System = text
			} else if text != "" {
				out.System += "\n\n" + text
			}
		case "user", "assistant":
			out.Messages = append(out.Messages, anthropicMessage{
				Role: msg.Role,
				Content: []anthropicContentBlock{
					{Type: "text", Text: text},
				},
			})
		default:
			return anthropicMessagesRequest{}, fmt.Errorf("unsupported message role: %s", msg.Role)
		}
	}

	if len(out.Messages) == 0 {
		return anthropicMessagesRequest{}, fmt.Errorf("at least one non-system message is required")
	}

	return out, nil
}

func openAIContentToText(v any) (string, error) {
	switch c := v.(type) {
	case string:
		return c, nil
	case []any:
		var parts []string
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				return "", fmt.Errorf("unsupported content block in messages")
			}
			t, _ := m["type"].(string)
			if t != "text" {
				return "", fmt.Errorf("only text content blocks are supported by the anthropic adapter")
			}
			text, _ := m["text"].(string)
			parts = append(parts, text)
		}
		return strings.Join(parts, "\n"), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("unsupported content format in messages")
	}
}

func toOpenAIResponse(requestModel string, in anthropicMessagesResponse) openAIChatCompletionResponse {
	model := in.Model
	if model == "" {
		model = requestModel
	}

	content := make([]string, 0, len(in.Content))
	for _, block := range in.Content {
		if block.Type == "text" {
			content = append(content, block.Text)
		}
	}

	promptTokens := in.Usage.InputTokens + in.Usage.CacheCreationInputTokens + in.Usage.CacheReadInputTokens
	completionTokens := in.Usage.OutputTokens

	return openAIChatCompletionResponse{
		ID:      in.ID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []openAIChoice{
			{
				Index: 0,
				Message: openAIAssistantMsg{
					Role:    "assistant",
					Content: strings.Join(content, "\n"),
				},
				FinishReason: mapStopReason(in.StopReason),
			},
		},
		Usage: openAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}
}

func mapStopReason(v string) string {
	switch v {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}
