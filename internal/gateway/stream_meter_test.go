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
	roleLine   = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`
	textLine   = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`
	finishLine = `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-4o-updated","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
)

// drain reads a StreamMeter to EOF and returns all bytes.
func drain(sm *StreamMeter) ([]byte, error) {
	return io.ReadAll(sm)
}

// noDone is a no-op onDone callback.
func noDone(_ string, _, _, _ int, _ bool) {}

// ---------------------------------------------------------------------------
// Data passthrough
// ---------------------------------------------------------------------------

func TestStreamMeterPassesThroughDataUnchanged(t *testing.T) {
	input := sseChunk(roleLine) + sseChunk(textLine) + sseChunk(finishLine) + "data: [DONE]\n\n"
	sm := newStreamMeter(sseBody(input), "gpt-4o", 0, 0, noDone)

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
	sm := newStreamMeter(sseBody(input), "initial-model", 0, 0, func(model string, _, _, _ int, _ bool) {
		gotModel = model
	})
	_, _ = drain(sm)

	if gotModel != "gpt-4o-updated" {
		t.Errorf("model: got %q, want gpt-4o-updated", gotModel)
	}
}

func TestStreamMeterFallsBackToInitialModelWhenNoneInStream(t *testing.T) {
	chunk := `{"id":"c1","object":"chat.completion.chunk","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
	input := sseChunk(chunk) + "data: [DONE]\n\n"

	var gotModel string
	sm := newStreamMeter(sseBody(input), "fallback-model", 0, 0, func(model string, _, _, _ int, _ bool) {
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
	sm := newStreamMeter(sseBody(input), "gpt-4o", 0, 0, func(_ string, p, c, tt int, _ bool) {
		prompt, completion, total = p, c, tt
	})
	_, _ = drain(sm)

	if prompt != 10 || completion != 5 || total != 15 {
		t.Errorf("tokens: got prompt=%d completion=%d total=%d, want 10/5/15",
			prompt, completion, total)
	}
}

func TestStreamMeterIgnoresChunkWithZeroTotal(t *testing.T) {
	first := `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":null}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}`
	second := `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`
	input := sseChunk(first) + sseChunk(second) + "data: [DONE]\n\n"

	var total int
	sm := newStreamMeter(sseBody(input), "m", 0, 0, func(_ string, _, _, tt int, _ bool) {
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
	input := sseChunk(textLine)
	var called int32
	sm := newStreamMeter(sseBody(input), "m", 0, 0, func(string, int, int, int, bool) {
		atomic.AddInt32(&called, 1)
	})
	_, _ = drain(sm)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("onDone called %d times, want 1", called)
	}
}

func TestStreamMeterOnDoneFiredOnDONELine(t *testing.T) {
	input := sseChunk(finishLine) + "data: [DONE]\n\n"
	var called int32
	sm := newStreamMeter(sseBody(input), "m", 0, 0, func(string, int, int, int, bool) {
		atomic.AddInt32(&called, 1)
	})
	_, _ = drain(sm)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("onDone called %d times, want 1", called)
	}
}

func TestStreamMeterOnDoneFiredOnClose(t *testing.T) {
	pr, pw := io.Pipe()

	var called int32
	sm := newStreamMeter(io.NopCloser(pr), "m", 0, 0, func(string, int, int, int, bool) {
		atomic.AddInt32(&called, 1)
	})

	go func() {
		_, _ = pw.Write([]byte(sseChunk(textLine)))
		pw.CloseWithError(errors.New("cancelled"))
	}()

	buf := make([]byte, 256)
	_, _ = sm.Read(buf)
	_ = sm.Close()

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("onDone called %d times, want 1", called)
	}
}

// ---------------------------------------------------------------------------
// Exactly-once guarantee
// ---------------------------------------------------------------------------

func TestStreamMeterOnDoneCalledExactlyOnce(t *testing.T) {
	input := sseChunk(finishLine) + "data: [DONE]\n\n"
	var called int32
	sm := newStreamMeter(sseBody(input), "m", 0, 0, func(string, int, int, int, bool) {
		atomic.AddInt32(&called, 1)
	})
	_, _ = drain(sm)
	_ = sm.Close()

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("onDone called %d times, want exactly 1", called)
	}
}

// ---------------------------------------------------------------------------
// Partial reads (lineBuf accumulation)
// ---------------------------------------------------------------------------

func TestStreamMeterHandlesPartialReads(t *testing.T) {
	input := sseChunk(finishLine) + "data: [DONE]\n\n"

	var prompt, total int
	sm := newStreamMeter(sseBody(input), "m", 0, 0, func(_ string, p, _, tt int, _ bool) {
		prompt, total = p, tt
	})

	buf := make([]byte, 1)
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

// ---------------------------------------------------------------------------
// Fallback estimation (no usage chunk)
// ---------------------------------------------------------------------------

func TestStreamMeterFallback_NoUsageChunk(t *testing.T) {
	contentChunk := `{"id":"c1","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello world"},"finish_reason":null}]}`
	input := sseChunk(contentChunk) + "data: [DONE]\n\n"

	var gotPrompt, gotCompletion, gotTotal int
	var gotEstimated bool
	sm := newStreamMeter(sseBody(input), "gpt-4o", 20, 0, func(_ string, p, c, tt int, est bool) {
		gotPrompt, gotCompletion, gotTotal = p, c, tt
		gotEstimated = est
	})
	_, _ = drain(sm)

	if !gotEstimated {
		t.Error("expected estimated=true when no usage chunk present")
	}
	if gotPrompt != 20 {
		t.Errorf("prompt: got %d, want 20", gotPrompt)
	}
	if gotCompletion != 2 { // 11 bytes / 4
		t.Errorf("completion: got %d, want 2 (11 bytes / 4)", gotCompletion)
	}
	if gotTotal != 22 {
		t.Errorf("total: got %d, want 22", gotTotal)
	}
}

func TestStreamMeterFallback_WithUsageChunk(t *testing.T) {
	input := sseChunk(textLine) + sseChunk(finishLine) + "data: [DONE]\n\n"

	var gotPrompt, gotCompletion, gotTotal int
	var gotEstimated bool
	sm := newStreamMeter(sseBody(input), "m", 100, 0, func(_ string, p, c, tt int, est bool) {
		gotPrompt, gotCompletion, gotTotal = p, c, tt
		gotEstimated = est
	})
	_, _ = drain(sm)

	if gotEstimated {
		t.Error("expected estimated=false when usage chunk is present")
	}
	if gotPrompt != 10 || gotCompletion != 5 || gotTotal != 15 {
		t.Errorf("tokens: got prompt=%d completion=%d total=%d, want 10/5/15",
			gotPrompt, gotCompletion, gotTotal)
	}
}

// ---------------------------------------------------------------------------
// Vision estimate in fallback path (no usage chunk)
// ---------------------------------------------------------------------------

// TestStreamMeterVisionEstimate_FallbackPath: vision estimate is added
// unconditionally to the fallback prompt when no usage chunk is present.
func TestStreamMeterVisionEstimate_FallbackPath(t *testing.T) {
	contentChunk := `{"id":"c1","model":"m","choices":[{"index":0,"delta":{"content":"Hi"},"finish_reason":null}]}`
	input := sseChunk(contentChunk) + "data: [DONE]\n\n"

	const promptEst = 20
	const visionEst = 3125

	var gotPrompt, gotTotal int
	var gotEstimated bool
	sm := newStreamMeter(sseBody(input), "m", promptEst, visionEst, func(_ string, p, _, tt int, est bool) {
		gotPrompt, gotTotal = p, tt
		gotEstimated = est
	})
	_, _ = drain(sm)

	if !gotEstimated {
		t.Error("expected estimated=true (fallback path)")
	}
	// prompt = promptEstimate + visionEstimate = 20 + 3125 = 3145
	wantPrompt := promptEst + visionEst
	if gotPrompt != wantPrompt {
		t.Errorf("prompt: got %d, want %d (promptEst + visionEst)", gotPrompt, wantPrompt)
	}
	// completion = len("Hi")/4 = 0; total = 3145 + 0
	if gotTotal != wantPrompt {
		t.Errorf("total: got %d, want %d", gotTotal, wantPrompt)
	}
}

// TestStreamMeterVisionEstimate_RealUsageZeroPrompt: when the stream has a
// real usage chunk but promptTokens == 0, visionEstimate is added.
func TestStreamMeterVisionEstimate_RealUsageZeroPrompt(t *testing.T) {
	// usage chunk: prompt=0, completion=5, total=5
	usageChunk := `{"id":"c1","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":0,"completion_tokens":5,"total_tokens":5}}`
	input := sseChunk(usageChunk) + "data: [DONE]\n\n"

	const visionEst = 765

	var gotPrompt, gotTotal int
	var gotEstimated bool
	sm := newStreamMeter(sseBody(input), "m", 0, visionEst, func(_ string, p, _, tt int, est bool) {
		gotPrompt, gotTotal = p, tt
		gotEstimated = est
	})
	_, _ = drain(sm)

	if gotEstimated {
		t.Error("expected estimated=false (real usage chunk present)")
	}
	if gotPrompt != visionEst {
		t.Errorf("prompt: got %d, want %d (visionEst added to zero prompt)", gotPrompt, visionEst)
	}
	if gotTotal != visionEst+5 {
		t.Errorf("total: got %d, want %d", gotTotal, visionEst+5)
	}
}

// TestStreamMeterVisionEstimate_RealUsageNonZeroPrompt: when the stream has a
// real usage chunk with a non-zero prompt count, visionEstimate is NOT added
// (provider already included image tokens in its count).
func TestStreamMeterVisionEstimate_RealUsageNonZeroPrompt(t *testing.T) {
	input := sseChunk(finishLine) + "data: [DONE]\n\n" // prompt=10, completion=5, total=15

	var gotPrompt, gotTotal int
	sm := newStreamMeter(sseBody(input), "m", 0, 999, func(_ string, p, _, tt int, _ bool) {
		gotPrompt, gotTotal = p, tt
	})
	_, _ = drain(sm)

	if gotPrompt != 10 {
		t.Errorf("prompt: got %d, want 10 (vision must not be added when prompt != 0)", gotPrompt)
	}
	if gotTotal != 15 {
		t.Errorf("total: got %d, want 15", gotTotal)
	}
}
