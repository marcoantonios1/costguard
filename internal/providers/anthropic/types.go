package anthropic

type openAIChatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stop        any             `json:"stop,omitempty"`
	User        string          `json:"user,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Tools       []openAITool    `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
}

type openAITool struct {
	Type     string           `json:"type"`
	Function openAIToolFunction `json:"function"`
}

type openAIToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicMessagesRequest struct {
	Model       string              `json:"model"`
	System      string              `json:"system,omitempty"`
	Messages    []anthropicMessage  `json:"messages"`
	MaxTokens   int                 `json:"max_tokens"`
	Temperature *float64            `json:"temperature,omitempty"`
	TopP        *float64            `json:"top_p,omitempty"`
	StopSeqs    []string            `json:"stop_sequences,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
	Tools       []anthropicTool     `json:"tools,omitempty"`
	ToolChoice  *anthropicToolChoice `json:"tool_choice,omitempty"`
}

type anthropicTool struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	InputSchema anthropicToolInputSchema  `json:"input_schema"`
}

type anthropicToolInputSchema struct {
	Type       string         `json:"type"`
	Properties any            `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicMessage struct {
	Role    string                   `json:"role"`
	Content []anthropicContentBlock  `json:"content"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicMessagesResponse struct {
	ID           string                   `json:"id"`
	Type         string                   `json:"type"`
	Role         string                   `json:"role"`
	Model        string                   `json:"model"`
	Content      []anthropicContentBlock  `json:"content"`
	StopReason   string                   `json:"stop_reason"`
	Usage        anthropicUsage           `json:"usage"`
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

type openAIChatCompletionResponse struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Created int64                `json:"created"`
	Model   string               `json:"model"`
	Choices []openAIChoice       `json:"choices"`
	Usage   openAIUsage          `json:"usage"`
}

type openAIChoice struct {
	Index        int                 `json:"index"`
	Message      openAIAssistantMsg  `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type openAIAssistantMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}