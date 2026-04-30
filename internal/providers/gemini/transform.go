package gemini

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/marcoantonios1/costguard/internal/providers/openaiformat"
)

// imageFetcher downloads a URL and returns the raw bytes and MIME type.
// The gateway supplies a real HTTP client; tests inject a stub.
type imageFetcher func(url string) (data []byte, mimeType string, err error)

var geminiSupportedMimeTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/heic": true,
	"image/heif": true,
}

func toGeminiRequest(in openAIChatCompletionRequest, fetch imageFetcher) (geminiGenerateContentRequest, error) {
	if in.Model == "" {
		return geminiGenerateContentRequest{}, fmt.Errorf("model is required")
	}
	if len(in.Messages) == 0 {
		return geminiGenerateContentRequest{}, fmt.Errorf("messages are required")
	}

	// Pre-build tool_call_id → function name lookup for tool result messages.
	toolCallNames := make(map[string]string)
	for _, msg := range in.Messages {
		for _, tc := range msg.ToolCalls {
			toolCallNames[tc.ID] = tc.Function.Name
		}
	}

	var systemTexts []string
	var contents []geminiContent

	for _, msg := range in.Messages {
		switch msg.Role {
		case "system":
			text, err := contentToText(msg.Content)
			if err != nil {
				return geminiGenerateContentRequest{}, err
			}
			if strings.TrimSpace(text) != "" {
				systemTexts = append(systemTexts, text)
			}

		case "user":
			parts, err := userContentToGeminiParts(msg.Content, fetch)
			if err != nil {
				return geminiGenerateContentRequest{}, err
			}
			if len(parts) == 0 {
				continue
			}
			contents = append(contents, geminiContent{Role: "user", Parts: parts})

		case "assistant":
			if len(msg.ToolCalls) > 0 {
				var parts []geminiPart
				if text, err := contentToText(msg.Content); err == nil && strings.TrimSpace(text) != "" {
					parts = append(parts, geminiPart{Text: text})
				}
				for _, tc := range msg.ToolCalls {
					var args any
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
					parts = append(parts, geminiPart{
						FunctionCall: &geminiFunctionCall{
							Name: tc.Function.Name,
							Args: args,
						},
					})
				}
				contents = append(contents, geminiContent{Role: "model", Parts: parts})
			} else {
				text, err := contentToText(msg.Content)
				if err != nil {
					return geminiGenerateContentRequest{}, err
				}
				if strings.TrimSpace(text) == "" {
					continue
				}
				contents = append(contents, geminiContent{
					Role:  "model",
					Parts: []geminiPart{{Text: text}},
				})
			}

		case "tool":
			content, _ := msg.Content.(string)
			name := toolCallNames[msg.ToolCallID]
			part := geminiPart{
				FunctionResponse: &geminiFunctionResponse{
					Name:     name,
					Response: map[string]any{"content": content},
				},
			}
			// Coalesce consecutive tool results into one user content (parallel tool calls).
			if n := len(contents); n > 0 && contents[n-1].Role == "user" && allFunctionResponses(contents[n-1].Parts) {
				contents[n-1].Parts = append(contents[n-1].Parts, part)
			} else {
				contents = append(contents, geminiContent{
					Role:  "user",
					Parts: []geminiPart{part},
				})
			}

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
			Parts: []geminiPart{{Text: strings.Join(systemTexts, "\n\n")}},
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

	toolConfig, omitTools, err := mapToolChoice(in.ToolChoice)
	if err != nil {
		return geminiGenerateContentRequest{}, err
	}

	if !omitTools && len(in.Tools) > 0 {
		var decls []geminiFunctionDeclaration
		for _, t := range in.Tools {
			decls = append(decls, geminiFunctionDeclaration{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
		out.Tools = []geminiTool{{FunctionDeclarations: decls}}
		out.ToolConfig = toolConfig
	}

	return out, nil
}

// mapToolChoice converts an OpenAI tool_choice to a Gemini toolConfig.
// Returns (toolConfig, omitTools, error). omitTools=true for "none".
func mapToolChoice(v any) (*geminiToolConfig, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	switch val := v.(type) {
	case string:
		switch val {
		case "none":
			return nil, true, nil
		case "auto":
			return &geminiToolConfig{FunctionCallingConfig: geminiToolCallingConfig{Mode: "AUTO"}}, false, nil
		case "required":
			return &geminiToolConfig{FunctionCallingConfig: geminiToolCallingConfig{Mode: "ANY"}}, false, nil
		default:
			return nil, false, fmt.Errorf("unsupported tool_choice string: %s", val)
		}
	case map[string]any:
		fn, _ := val["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if name == "" {
			return nil, false, fmt.Errorf("tool_choice object missing function.name")
		}
		return &geminiToolConfig{
			FunctionCallingConfig: geminiToolCallingConfig{
				Mode:                 "ANY",
				AllowedFunctionNames: []string{name},
			},
		}, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported tool_choice format")
	}
}

func allFunctionResponses(parts []geminiPart) bool {
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		if p.FunctionResponse == nil {
			return false
		}
	}
	return true
}

func toOpenAIResponse(requestModel string, in geminiGenerateContentResponse) openAIChatCompletionResponse {
	finishReason := "stop"
	msg := openAIAssistantMsg{Role: "assistant"}

	if len(in.Candidates) > 0 {
		c := in.Candidates[0]
		finishReason = normalizeFinishReason(c.FinishReason)

		var textParts []string
		var toolCalls []openAIToolCall

		for _, p := range c.Content.Parts {
			if p.FunctionCall != nil {
				args, _ := json.Marshal(p.FunctionCall.Args)
				toolCalls = append(toolCalls, openAIToolCall{
					// Gemini doesn't return a call id; synthesise one from the function name.
					ID:   "call_" + p.FunctionCall.Name,
					Type: "function",
					Function: openAIToolCallFunction{
						Name:      p.FunctionCall.Name,
						Arguments: string(args),
					},
				})
			} else if strings.TrimSpace(p.Text) != "" {
				textParts = append(textParts, p.Text)
			}
		}

		if len(toolCalls) > 0 {
			msg.ToolCalls = toolCalls
			msg.Content = nil
			finishReason = "tool_calls"
		} else {
			text := strings.Join(textParts, "\n")
			msg.Content = &text
		}
	} else {
		empty := ""
		msg.Content = &empty
	}

	return openAIChatCompletionResponse{
		ID:      fallbackString(in.ResponseID, fmt.Sprintf("chatcmpl-gemini-%d", time.Now().UnixNano())),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   fallbackString(in.ModelVersion, requestModel),
		Choices: []openAIChoice{
			{
				Index:        0,
				Message:      msg,
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

// translateGeminiStream reads Gemini SSE events from r and writes
// OpenAI-format SSE chunks to w, ending with "data: [DONE]".
// Gemini streams bare "data: <json>" lines; each is a partial GenerateContentResponse.
func translateGeminiStream(requestModel string, r io.Reader, w io.Writer) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	id := fmt.Sprintf("chatcmpl-gemini-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	model := requestModel
	sentRole := false
	toolCallIdx := 0

	writeChunk := func(chunk openAIStreamChunk) {
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
	}

	base := func() openAIStreamChunk {
		return openAIStreamChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   model,
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var partial geminiGenerateContentResponse
		if json.Unmarshal([]byte(data), &partial) != nil {
			continue
		}

		if partial.ModelVersion != "" {
			model = partial.ModelVersion
		}

		if len(partial.Candidates) == 0 {
			continue
		}

		candidate := partial.Candidates[0]

		// Emit role chunk once, before first content.
		if !sentRole {
			sentRole = true
			role := "assistant"
			chunk := base()
			chunk.Choices = []openAIStreamChoice{{
				Index:        0,
				Delta:        openAIStreamDelta{Role: role},
				FinishReason: nil,
			}}
			writeChunk(chunk)
		}

		// Emit content/tool-call parts.
		for _, part := range candidate.Content.Parts {
			if part.FunctionCall != nil {
				args, _ := json.Marshal(part.FunctionCall.Args)
				chunk := base()
				chunk.Choices = []openAIStreamChoice{{
					Index: 0,
					Delta: openAIStreamDelta{
						ToolCalls: []openAIStreamToolCall{{
							Index: toolCallIdx,
							ID:    "call_" + part.FunctionCall.Name,
							Type:  "function",
							Function: openAIStreamToolCallFunction{
								Name:      part.FunctionCall.Name,
								Arguments: string(args),
							},
						}},
					},
					FinishReason: nil,
				}}
				writeChunk(chunk)
				toolCallIdx++
			} else if strings.TrimSpace(part.Text) != "" {
				text := part.Text
				chunk := base()
				chunk.Choices = []openAIStreamChoice{{
					Index:        0,
					Delta:        openAIStreamDelta{Content: &text},
					FinishReason: nil,
				}}
				writeChunk(chunk)
			}
		}

		// Emit finish chunk when Gemini signals the end.
		if candidate.FinishReason != "" {
			fr := normalizeFinishReason(candidate.FinishReason)
			if toolCallIdx > 0 {
				fr = "tool_calls"
			}
			chunk := base()
			chunk.Choices = []openAIStreamChoice{{
				Index:        0,
				Delta:        openAIStreamDelta{},
				FinishReason: &fr,
			}}
			if partial.UsageMetadata.TotalTokenCount > 0 {
				chunk.Usage = &openAIStreamUsage{
					PromptTokens:     partial.UsageMetadata.PromptTokenCount,
					CompletionTokens: partial.UsageMetadata.CandidatesTokenCount,
					TotalTokens:      partial.UsageMetadata.TotalTokenCount,
				}
			}
			writeChunk(chunk)
		}
	}

	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
}

func userContentToGeminiParts(v any, fetch imageFetcher) ([]geminiPart, error) {
	parsed, err := openaiformat.ParseContentParts(v)
	if err != nil {
		return nil, err
	}
	parts := make([]geminiPart, 0, len(parsed))
	for _, p := range parsed {
		switch p.Type {
		case "text":
			if strings.TrimSpace(p.Text) != "" {
				parts = append(parts, geminiPart{Text: p.Text})
			}
		case "image_url":
			gp, err := imageURLToGeminiPart(p.ImageURL.URL, fetch)
			if err != nil {
				return nil, err
			}
			parts = append(parts, gp)
		}
	}
	return parts, nil
}

func imageURLToGeminiPart(rawURL string, fetch imageFetcher) (geminiPart, error) {
	if strings.HasPrefix(rawURL, "data:") {
		rest := strings.TrimPrefix(rawURL, "data:")
		semicolonIdx := strings.IndexByte(rest, ';')
		if semicolonIdx < 0 {
			return geminiPart{}, fmt.Errorf("malformed data URI: missing semicolon")
		}
		mimeType := rest[:semicolonIdx]
		encodingAndData := rest[semicolonIdx+1:]
		commaIdx := strings.IndexByte(encodingAndData, ',')
		if commaIdx < 0 {
			return geminiPart{}, fmt.Errorf("malformed data URI: missing comma")
		}
		if encodingAndData[:commaIdx] != "base64" {
			return geminiPart{}, fmt.Errorf("data URI encoding %q is not supported; only base64 is accepted", encodingAndData[:commaIdx])
		}
		if !geminiSupportedMimeTypes[mimeType] {
			return geminiPart{}, fmt.Errorf("unsupported image media type %q; accepted: image/jpeg, image/png, image/gif, image/webp, image/heic, image/heif", mimeType)
		}
		return geminiPart{
			InlineData: &geminiInlineData{
				MimeType: mimeType,
				Data:     encodingAndData[commaIdx+1:],
			},
		}, nil
	}

	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return geminiPart{}, fmt.Errorf("image_url must be a data URI or an http(s) URL")
	}
	if fetch == nil {
		return geminiPart{}, fmt.Errorf("cannot fetch image URL %q: no HTTP fetcher configured", rawURL)
	}
	data, mimeType, err := fetch(rawURL)
	if err != nil {
		return geminiPart{}, fmt.Errorf("failed to fetch image %q: %w", rawURL, err)
	}
	if !geminiSupportedMimeTypes[mimeType] {
		return geminiPart{}, fmt.Errorf("unsupported image media type %q fetched from %q", mimeType, rawURL)
	}
	return geminiPart{
		InlineData: &geminiInlineData{
			MimeType: mimeType,
			Data:     base64.StdEncoding.EncodeToString(data),
		},
	}, nil
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
