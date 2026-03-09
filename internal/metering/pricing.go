package metering

type Price struct {
	InputPer1M       float64
	CachedInputPer1M float64
	OutputPer1M      float64
}

var Prices = map[string]map[string]Price{
	// --------------------
	// OpenAI
	// --------------------
	"openai": {
		"gpt-5": {
			InputPer1M:       1.25,
			CachedInputPer1M: 0.125,
			OutputPer1M:      10.00,
		},
		"gpt-5-mini": {
			InputPer1M:       0.25,
			CachedInputPer1M: 0.025,
			OutputPer1M:      2.00,
		},
		"gpt-5-nano": {
			InputPer1M:       0.05,
			CachedInputPer1M: 0.005,
			OutputPer1M:      0.40,
		},
		"gpt-4o": {
			InputPer1M:       2.50,
			CachedInputPer1M: 1.25,
			OutputPer1M:      10.00,
		},
		"gpt-4o-mini": {
			InputPer1M:       0.15,
			CachedInputPer1M: 0.075,
			OutputPer1M:      0.60,
		},
		"gpt-4.1": {
			InputPer1M:       2.00,
			CachedInputPer1M: 0.50,
			OutputPer1M:      8.00,
		},
		"gpt-4.1-mini": {
			InputPer1M:       0.40,
			CachedInputPer1M: 0.10,
			OutputPer1M:      1.60,
		},
		"gpt-4.1-nano": {
			InputPer1M:       0.10,
			CachedInputPer1M: 0.025,
			OutputPer1M:      0.40,
		},
		"o3": {
			InputPer1M:       2.00,
			CachedInputPer1M: 0.50,
			OutputPer1M:      8.00,
		},
		"o4-mini": {
			InputPer1M:       1.10,
			CachedInputPer1M: 0.275,
			OutputPer1M:      4.40,
		},
	},

	// --------------------
	// Anthropic
	// --------------------
	"anthropic": {
		"claude-haiku-4-5": {
			InputPer1M:  1.00,
			OutputPer1M: 5.00,
		},
		"claude-sonnet-4-5": {
			InputPer1M:  3.00,
			OutputPer1M: 15.00,
		},
		"claude-opus-4-6": {
			InputPer1M:  5.00,
			OutputPer1M: 25.00,
		},
	},

	// --------------------
	// Google Gemini
	// --------------------
	"google": {
		"gemini-2.5-flash-lite": {
			InputPer1M:  0.10,
			OutputPer1M: 0.40,
		},
		"gemini-2.5-flash": {
			InputPer1M:  0.30,
			OutputPer1M: 2.50,
		},
		"gemini-2.5-pro": {
			InputPer1M:  1.25,
			OutputPer1M: 10.00,
		},
	},

	// --------------------
	// xAI
	// --------------------
	"xai": {
		"grok-4-1-fast-reasoning": {
			InputPer1M:  0.20,
			OutputPer1M: 0.50,
		},
		"grok-4-1-fast-non-reasoning": {
			InputPer1M:  0.20,
			OutputPer1M: 0.50,
		},
	},
}
