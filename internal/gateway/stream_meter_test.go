package gateway

import (
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

// sseBody builds an io.NopCloser from a sequence of raw SSE lines.
func sseBody(lines ...string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(strings.Join(lines, "")))
}

// chunk returns an SSE data line with a trailing \n\n.
func sseChunk(json string) string { return "data: " + json + "\n\n" }

const (
	roleLine  = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
	textLine  = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`
	finishLine = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o-updated","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
)

// drain reads a StreamMeter to EOF and returns all bytes.
func drain(sm *StreamMeter) ([]byte, error) {
	return io.ReadAll(sm)
}

// ---------------------------------------------------------------------------
// Data passthrough
// ---------------------------------------------------------------------------

func TestStreamMeterPassesThroughDataUnchanged(t *testing.T) {
	input := sseChunk(roleLine) + sseChunk(textLine) + sseChunk(finishLine) + "data: [DONE]\n\n"
	sm := newStreamMeter(sseBody(input), "gpt-4o", func(string, int, int, int) {})

	got, err := drain(sm)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if string(got) != input {
		t.Errorf("data mismatch\ngot:  %q\nwant: %q", got, input)
	}
}

// ---------------------------------------------------------------------------
// Model extraction
// ---------------------------------------------------------------------------

func TestStreamMeterPicksUpModelFromChunk(t *testing.T) {
	input := sseChunk(roleLine) + sseChunk(finishLine) + "data: [DONE]\n\n"

	var gotModel string
	sm := newStreamMeter(sseBody(input), "initial-model", func(model string, _, _, _ int) {
		gotModel = model
	})
	_, _ = drain(sm)

	// finishLine carries "gpt-4o-updated"
	if gotModel != "gpt-4o-updated" {
		t.Errorf("model: got %q, want gpt-4o-updated", gotModel)
	}
}

func TestStreamMeterFallsBackToInitialModelWhenNoneInStream(t *testing.T) {
	chunk := `{"id":"c1","object":"chat.completion.chunk","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
	input := sseChunk(chunk) + "data: [DONE]\n\n"

	var gotModel string
	sm := newStreamMeter(sseBody(input), "fallback-model", func(model string, _, _, _ int) {
		gotModel = model
	})
	_, _ = drain(sm)

	if gotModel != "fallback-model" {
		t.Errorf("model: got %q, want fallback-model", gotModel)
	}
}

// ---------------------------------------------------------------------------
// Token collection
// ---------------------------------------------------------------------------

func TestStreamMeterCollectsTokensFromUsageChunk(t *testing.T) {
	input := sseChunk(roleLine) + sseChunk(textLine) + sseChunk(finishLine) + "data: [DONE]\n\n"

	var prompt, completion, total int
	sm := newStreamMeter(sseBody(input), "gpt-4o", func(_ string, p, c, tt int) {
		prompt, completion, total = p, c, tt
	})
	_, _ = drain(sm)

	if prompt != 10 || completion != 5 || total != 15 {
		t.Errorf("tokens: got prompt=%d completion=%d total=%d, want 10/5/15",
			prompt, completion, total)
	}
}

func TestStreamMeterIgnoresChunkWithZeroTotal(t *testing.T) {
	// A chunk with usage but total_tokens == 0 should not overwrite a
	// previously seen non-zero count.
	first := `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`
	second := `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
	input := sseChunk(first) + sseChunk(second) + "data: [DONE]\n\n"

	var total int
	sm := newStreamMeter(sseBody(input), "m", func(_ string, _, _, tt int) {
		total = tt
	})
	_, _ = drain(sm)

	if total != 12 {
		t.Errorf("total: got %d, want 12", total)
	}
}

// ---------------------------------------------------------------------------
// onDone trigger conditions
// ---------------------------------------------------------------------------

func TestStreamMeterOnDoneFiredOnEOF(t *testing.T) {
	input := sseChunk(textLine) // no [DONE], just EOF
	var called int32
	sm := newStreamMeter(sseBody(input), "m", func(string, int, int, int) {
		atomic.AddInt32(&called, 1)
	})
	_, _ = drain(sm)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("onDone called %d times, want 1", called)
	}
}

func TestStreamMeterOnDoneFiredOnDONELine(t *testing.T) {
	// Finish chunk with usage comes before [DONE]; onDone should fire on [DONE].
	input := sseChunk(finishLine) + "data: [DONE]\n\n"
	var called int32
	sm := newStreamMeter(sseBody(input), "m", func(string, int, int, int) {
		atomic.AddInt32(&called, 1)
	})
	_, _ = drain(sm)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("onDone called %d times, want 1", called)
	}
}

func TestStreamMeterOnDoneFiredOnClose(t *testing.T) {
	// Simulate a client that reads only part of the stream then closes.
	pr, pw := io.Pipe()

	var called int32
	sm := newStreamMeter(io.NopCloser(pr), "m", func(string, int, int, int) {
		atomic.AddInt32(&called, 1)
	})

	// Write one chunk and close the write end without [DONE].
	go func() {
		_, _ = pw.Write([]byte(sseChunk(textLine)))
		pw.CloseWithError(errors.New("cancelled"))
	}()

	buf := make([]byte, 256)
	_, _ = sm.Read(buf) // consume the chunk
	_ = sm.Close()      // explicit close

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("onDone called %d times, want 1", called)
	}
}

// ---------------------------------------------------------------------------
// Exactly-once guarantee
// ---------------------------------------------------------------------------

func TestStreamMeterOnDoneCalledExactlyOnce(t *testing.T) {
	// [DONE] fires finish(), then EOF fires finish() again — only one call expected.
	input := sseChunk(finishLine) + "data: [DONE]\n\n"
	var called int32
	sm := newStreamMeter(sseBody(input), "m", func(string, int, int, int) {
		atomic.AddInt32(&called, 1)
	})
	_, _ = drain(sm)
	_ = sm.Close() // extra Close should not fire again

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("onDone called %d times, want exactly 1", called)
	}
}

// ---------------------------------------------------------------------------
// Partial reads (lineBuf accumulation)
// ---------------------------------------------------------------------------

func TestStreamMeterHandlesPartialReads(t *testing.T) {
	// Feed the SSE stream one byte at a time to exercise lineBuf logic.
	input := sseChunk(finishLine) + "data: [DONE]\n\n"

	var prompt, total int
	sm := newStreamMeter(sseBody(input), "m", func(_ string, p, _, tt int) {
		prompt, total = p, tt
	})

	buf := make([]byte, 1) // one byte at a time
	for {
		_, err := sm.Read(buf)
		if err != nil {
			break
		}
	}

	if prompt != 10 || total != 15 {
		t.Errorf("tokens: got prompt=%d total=%d, want 10/15", prompt, total)
	}
}
