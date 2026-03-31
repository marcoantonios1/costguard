package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
)

// StreamMeter wraps a streaming SSE response body. As data passes through
// Read it inspects each line for model and usage fields without allocating
// an extra copy for the caller. When the stream ends — via [DONE], EOF, or
// Close — it fires onDone exactly once with the accumulated token counts.
type StreamMeter struct {
	src     io.ReadCloser
	lineBuf []byte // holds bytes seen since the last \n

	mu               sync.Mutex
	model            string
	promptTokens     int
	completionTokens int
	totalTokens      int

	doneOnce sync.Once
	onDone   func(model string, prompt, completion, total int)
}

func newStreamMeter(src io.ReadCloser, initialModel string, onDone func(string, int, int, int)) *StreamMeter {
	return &StreamMeter{
		src:    src,
		model:  initialModel,
		onDone: onDone,
	}
}

// Read implements io.Reader. The bytes are copied directly into p (no extra
// allocation), then the same slice is inspected for SSE usage lines.
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
		model, prompt, completion, total :=
			sm.model, sm.promptTokens, sm.completionTokens, sm.totalTokens
		sm.mu.Unlock()
		sm.onDone(model, prompt, completion, total)
	})
}

// inspect feeds freshly-read bytes into a line buffer and processes each
// complete \n-terminated line as it arrives.
func (sm *StreamMeter) inspect(data []byte) {
	sm.lineBuf = append(sm.lineBuf, data...)
	for {
		idx := bytes.IndexByte(sm.lineBuf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimRight(string(sm.lineBuf[:idx]), "\r")
		sm.lineBuf = sm.lineBuf[idx+1:]
		sm.processLine(line)
	}
}

// processLine extracts model and usage from "data: <json>" SSE lines.
// Fires finish on [DONE].
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
		Model string `json:"model"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
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
	sm.mu.Unlock()
}
