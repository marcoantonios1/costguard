package anthropic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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
	out := anthropicMessagesRequest{
		Model:     in.Model,
		MaxTokens: in.MaxTokens,
		Stream:    in.Stream,
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
		switch msg.Role {
		case "system":
			text, err := openAIContentToText(msg.Content)
			if err != nil {
				return anthropicMessagesRequest{}, err
			}
			if out.System == "" {
				out.System = text
			} else if text != "" {
				out.System += "\n\n" + text
			}

		case "assistant":
			if len(msg.ToolCalls) > 0 {
				var blocks []anthropicContentBlock
				if text, err := openAIContentToText(msg.Content); err == nil && text != "" {
					blocks = append(blocks, anthropicContentBlock{Type: "text", Text: text})
				}
				for _, tc := range msg.ToolCalls {
					var input any
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
					blocks = append(blocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: input,
					})
				}
				out.Messages = append(out.Messages, anthropicMessage{Role: "assistant", Content: blocks})
			} else {
				text, err := openAIContentToText(msg.Content)
				if err != nil {
					return anthropicMessagesRequest{}, err
				}
				out.Messages = append(out.Messages, anthropicMessage{
					Role:    "assistant",
					Content: []anthropicContentBlock{{Type: "text", Text: text}},
				})
			}

		case "user":
			text, err := openAIContentToText(msg.Content)
			if err != nil {
				return anthropicMessagesRequest{}, err
			}
			out.Messages = append(out.Messages, anthropicMessage{
				Role:    "user",
				Content: []anthropicContentBlock{{Type: "text", Text: text}},
			})

		case "tool":
			content, _ := msg.Content.(string)
			block := anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   content,
			}
			// Coalesce consecutive tool results into a single Anthropic user message
			// (required for parallel tool calls).
			if n := len(out.Messages); n > 0 && out.Messages[n-1].Role == "user" && allToolResults(out.Messages[n-1].Content) {
				out.Messages[n-1].Content = append(out.Messages[n-1].Content, block)
			} else {
				out.Messages = append(out.Messages, anthropicMessage{
					Role:    "user",
					Content: []anthropicContentBlock{block},
				})
			}

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
func allToolResults(blocks []anthropicContentBlock) bool {
	if len(blocks) == 0 {
		return false
	}
	for _, b := range blocks {
		if b.Type != "tool_result" {
			return false
		}
	}
	return true
}

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

	var textParts []string
	var toolCalls []openAIToolCall

	for _, block := range in.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, openAIToolCall{
				ID:   block.ID,
				Type: "function",
				Function: openAIToolCallFunction{
					Name:      block.Name,
					Arguments: string(args),
				},
			})
		}
	}

	msg := openAIAssistantMsg{Role: "assistant"}

	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		// OpenAI sets content to null when tool_calls are present
		msg.Content = nil
	} else {
		text := strings.Join(textParts, "\n")
		msg.Content = &text
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
				Index:        0,
				Message:      msg,
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

// translateAnthropicStream reads Anthropic SSE events from r and writes
// OpenAI-format SSE chunks to w, ending with "data: [DONE]".
func translateAnthropicStream(requestModel string, r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var (
		messageID        string
		model            = requestModel
		created          = time.Now().Unix()
		blockTypes       = map[int]string{} // content block index → "text" | "tool_use"
		toolCallIdx      = map[int]int{}    // content block index → tool_calls array index
		nextToolIdx      = 0
		eventType        string
		inputTokens      int
		outputTokens     int
	)

	writeChunk := func(chunk openAIStreamChunk) {
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}

	base := func() openAIStreamChunk {
		return openAIStreamChunk{
			ID:      messageID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
		}
	}

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		switch eventType {
		case "message_start":
			var e struct {
				Message struct {
					ID    string `json:"id"`
					Model string `json:"model"`
					Usage struct {
						InputTokens int `json:"input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(data), &e) != nil {
				continue
			}
			messageID = e.Message.ID
			if e.Message.Model != "" {
				model = e.Message.Model
			}
			inputTokens = e.Message.Usage.InputTokens
			chunk := base()
			role := "assistant"
			chunk.Choices = []openAIStreamChoice{{
				Index:        0,
				Delta:        openAIStreamDelta{Role: role},
				FinishReason: nil,
			}}
			writeChunk(chunk)

		case "content_block_start":
			var e struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"content_block"`
			}
			if json.Unmarshal([]byte(data), &e) != nil {
				continue
			}
			blockTypes[e.Index] = e.ContentBlock.Type
			if e.ContentBlock.Type == "tool_use" {
				tcIdx := nextToolIdx
				toolCallIdx[e.Index] = tcIdx
				nextToolIdx++
				chunk := base()
				chunk.Choices = []openAIStreamChoice{{
					Index: 0,
					Delta: openAIStreamDelta{
						ToolCalls: []openAIStreamToolCall{{
							Index: tcIdx,
							ID:    e.ContentBlock.ID,
							Type:  "function",
							Function: openAIStreamToolCallFunction{
								Name:      e.ContentBlock.Name,
								Arguments: "",
							},
						}},
					},
					FinishReason: nil,
				}}
				writeChunk(chunk)
			}

		case "content_block_delta":
			var e struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(data), &e) != nil {
				continue
			}
			chunk := base()
			switch e.Delta.Type {
			case "text_delta":
				text := e.Delta.Text
				chunk.Choices = []openAIStreamChoice{{
					Index:        0,
					Delta:        openAIStreamDelta{Content: &text},
					FinishReason: nil,
				}}
				writeChunk(chunk)
			case "input_json_delta":
				tcIdx := toolCallIdx[e.Index]
				chunk.Choices = []openAIStreamChoice{{
					Index: 0,
					Delta: openAIStreamDelta{
						ToolCalls: []openAIStreamToolCall{{
							Index: tcIdx,
							Function: openAIStreamToolCallFunction{
								Arguments: e.Delta.PartialJSON,
							},
						}},
					},
					FinishReason: nil,
				}}
				writeChunk(chunk)
			}

		case "message_delta":
			var e struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(data), &e) != nil {
				continue
			}
			outputTokens = e.Usage.OutputTokens
			fr := mapStopReason(e.Delta.StopReason)
			total := inputTokens + outputTokens
			chunk := base()
			chunk.Choices = []openAIStreamChoice{{
				Index:        0,
				Delta:        openAIStreamDelta{},
				FinishReason: &fr,
			}}
			chunk.Usage = &openAIStreamUsage{
				PromptTokens:     inputTokens,
				CompletionTokens: outputTokens,
				TotalTokens:      total,
			}
			writeChunk(chunk)

		case "message_stop":
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		}
	}

	_ = blockTypes // used for future block-type-aware logic
}
