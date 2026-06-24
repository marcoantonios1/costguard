package metering

type Usage struct {
	Provider                 string
	Model                    string
	PromptTokens             int // base (non-cache) input tokens only
	CompletionTokens         int
	TotalTokens              int
	CacheHit                 bool
	CacheCreationInputTokens int
	CacheReadInputTokens     int
}
