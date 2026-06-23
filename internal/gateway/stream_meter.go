package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
)

// maxLineBufSize caps how many bytes a single unterminated SSE line may
// accumulate in lineBuf. If a line exceeds this (e.g. a mislabeled non-SSE
// body or a pathologically long line), the buffered bytes are dropped and
// the line is skipped for metering. Data passthrough via Read is unaffected.
const maxLineBufSize = 1 << 20 // 1 MB

// StreamMeter wraps a streaming SSE response body. As data passes through
// Read it inspects each line for model, usage, and content delta fields
// without allocating an extra copy for the caller. When the stream ends —
// via [DONE], EOF, or Close — it fires onDone exactly once.
//
// Token accounting at finish time (applied in order):
//
//  1. If the stream contained a usage chunk with total > 0 (real counts):
//     - if promptTokens == 0 and visionEstimate > 0, add visionEstimate to
//       promptTokens (same guard as the non-streaming path).
//
//  2. If the stream ended with total == 0 (no usage chunk, e.g. Gemini SSE):
//     - Apply fallback: completion ≈ accumulatedTextLen/4,
//       prompt = promptEstimate + visionEstimate (unconditional).
//     - Sets estimated=true in the onDone callback.
type StreamMeter struct {
	src            io.ReadCloser
	lineBuf        []byte
	lineOverflowed bool // true while draining an oversized line past the cap
	promptEstimate int
	visionEstimate int

	mu                 sync.Mutex
	model              string
	promptTokens       int
	completionTokens   int
	totalTokens        int
	accumulatedTextLen int

	doneOnce sync.Once
	onDone   func(model string, prompt, completion, total int, estimated bool)
}

func newStreamMeter(
	src io.ReadCloser,
	initialModel string,
	promptEstimate int,
	visionEstimate int,
	onDone func(string, int, int, int, bool),
) *StreamMeter {
	return &StreamMeter{
		src:            src,
		model:          initialModel,
		promptEstimate: promptEstimate,
		visionEstimate: visionEstimate,
		onDone:         onDone,
	}
}

// Read implements io.Reader. Bytes are copied directly into p (no extra
// allocation), then the same slice is inspected for SSE lines.
func (sm *StreamMeter) Read(p []byte) (int, error) {
	n, err := sm.src.Read(p)
	if n > 0 {
		sm.inspect(p[:n])
	}
	if err != nil {
		sm.finish()
	}
	return n, err
}

// Close implements io.Closer. Ensures onDone fires even when the client
// disconnects before the stream ends naturally.
func (sm *StreamMeter) Close() error {
	sm.finish()
	return sm.src.Close()
}

func (sm *StreamMeter) finish() {
	sm.doneOnce.Do(func() {
		sm.mu.Lock()
		model := sm.model
		prompt, completion, total := sm.promptTokens, sm.completionTokens, sm.totalTokens
		estimated := false

		if total == 0 {
			// No usage chunk in stream (e.g. Gemini): apply fallback estimation.
			// Vision estimate is added unconditionally to the fallback prompt.
			completion = sm.accumulatedTextLen / 4
			prompt = sm.promptEstimate + sm.visionEstimate
			total = prompt + completion
			estimated = true
		} else if prompt == 0 && sm.visionEstimate > 0 {
			// Real usage reported but prompt tokens were omitted (some providers
			// zero out prompt in streaming). Add the vision estimate and recompute
			// total — same guard as the non-streaming meterResponse path.
			prompt = sm.visionEstimate
			total = prompt + completion
		}
		sm.mu.Unlock()
		sm.onDone(model, prompt, completion, total, estimated)
	})
}

// inspect feeds freshly-read bytes into a line buffer and processes each
// complete \n-terminated line as it arrives.
func (sm *StreamMeter) inspect(data []byte) {
	sm.lineBuf = append(sm.lineBuf, data...)
	for {
		idx := bytes.IndexByte(sm.lineBuf, '\n')
		if idx < 0 {
			// No newline yet. If the unterminated line exceeds the cap, drop
			// the buffered bytes and mark it as overflowed so it is skipped
			// once the eventual newline arrives. This bounds memory regardless
			// of how long the line ultimately grows.
			if len(sm.lineBuf) > maxLineBufSize {
				sm.lineBuf = sm.lineBuf[:0]
				sm.lineOverflowed = true
			}
			break
		}

		line := sm.lineBuf[:idx]
		sm.lineBuf = sm.lineBuf[idx+1:]

		if sm.lineOverflowed {
			// This newline terminates the line we gave up on; skip it and
			// resume normal buffering for whatever follows.
			sm.lineOverflowed = false
			continue
		}

		sm.processLine(strings.TrimRight(string(line), "\r"))
	}
}

// processLine extracts model, usage, and content deltas from "data: <json>"
// SSE lines. Fires finish on [DONE].
func (sm *StreamMeter) processLine(line string) {
	if !strings.HasPrefix(line, "data: ") {
		return
	}
	payload := strings.TrimPrefix(line, "data: ")
	if payload == "[DONE]" {
		sm.finish()
		return
	}

	var chunk struct {
		Model   string `json:"model"`
		Usage   *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if json.Unmarshal([]byte(payload), &chunk) != nil {
		return
	}

	sm.mu.Lock()
	if chunk.Model != "" {
		sm.model = chunk.Model
	}
	if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
		sm.promptTokens = chunk.Usage.PromptTokens
		sm.completionTokens = chunk.Usage.CompletionTokens
		sm.totalTokens = chunk.Usage.TotalTokens
	}
	for _, choice := range chunk.Choices {
		sm.accumulatedTextLen += len(choice.Delta.Content)
	}
	sm.mu.Unlock()
}
