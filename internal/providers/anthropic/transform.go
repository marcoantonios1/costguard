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

	toolChoice, omitTools, err := mapToolChoice(in.ToolChoice)
	if err != nil {
		return anthropicMessagesRequest{}, err
	}

	if !omitTools && len(in.Tools) > 0 {
		for _, t := range in.Tools {
			out.Tools = append(out.Tools, anthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: anthropicToolInputSchema{
					Type:       schemaType(t.Function.Parameters),
					Properties: schemaProperties(t.Function.Parameters),
					Required:   schemaRequired(t.Function.Parameters),
				},
			})
		}
		out.ToolChoice = toolChoice
	}

	return out, nil
}

// mapToolChoice converts an OpenAI tool_choice value to its Anthropic equivalent.
// Returns (toolChoice, omitTools, error). omitTools=true for "none".
func mapToolChoice(v any) (*anthropicToolChoice, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	switch val := v.(type) {
	case string:
		switch val {
		case "none":
			return nil, true, nil
		case "auto":
			return &anthropicToolChoice{Type: "auto"}, false, nil
		case "required":
			return &anthropicToolChoice{Type: "any"}, false, nil
		default:
			return nil, false, fmt.Errorf("unsupported tool_choice string: %s", val)
		}
	case map[string]any:
		fn, _ := val["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if name == "" {
			return nil, false, fmt.Errorf("tool_choice object missing function.name")
		}
		return &anthropicToolChoice{Type: "tool", Name: name}, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported tool_choice format")
	}
}

func schemaType(params any) string {
	m, ok := params.(map[string]any)
	if !ok {
		return "object"
	}
	t, _ := m["type"].(string)
	if t == "" {
		return "object"
	}
	return t
}

func schemaProperties(params any) any {
	m, ok := params.(map[string]any)
	if !ok {
		return nil
	}
	return m["properties"]
}

func schemaRequired(params any) []string {
	m, ok := params.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := m["required"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		if s, ok := r.(string); ok {
			out = append(out, s)
		}
	}
	return out
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
