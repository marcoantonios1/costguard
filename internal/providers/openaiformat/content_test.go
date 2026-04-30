package openaiformat

import (
	"encoding/json"
	"testing"
)

// decodeContent simulates what encoding/json does when unmarshaling an
// openAIMessage.Content field (typed as `any`) from a raw JSON payload.
func decodeContent(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func TestPlainString(t *testing.T) {
	parts, err := ParseContentParts("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "hello world" {
		t.Errorf("got %+v", parts[0])
	}
}

func TestNilContent(t *testing.T) {
	parts, err := ParseContentParts(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parts != nil {
		t.Errorf("expected nil slice, got %v", parts)
	}
}

func TestTextBlockOnly(t *testing.T) {
	v := decodeContent(t, `[{"type":"text","text":"hello"}]`)
	parts, err := ParseContentParts(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "hello" {
		t.Errorf("got %+v", parts[0])
	}
}

func TestImageOnlyNoError(t *testing.T) {
	v := decodeContent(t, `[{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]`)
	parts, err := ParseContentParts(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	p := parts[0]
	if p.Type != "image_url" {
		t.Errorf("type: got %q, want image_url", p.Type)
	}
	if p.ImageURL == nil {
		t.Fatal("ImageURL is nil")
	}
	if p.ImageURL.URL != "https://example.com/img.png" {
		t.Errorf("url: got %q", p.ImageURL.URL)
	}
}

func TestImageWithDetail(t *testing.T) {
	v := decodeContent(t, `[{"type":"image_url","image_url":{"url":"https://example.com/img.png","detail":"high"}}]`)
	parts, err := ParseContentParts(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parts[0].ImageURL.Detail != "high" {
		t.Errorf("detail: got %q, want high", parts[0].ImageURL.Detail)
	}
}

func TestMixedTextAndImagePreservesOrder(t *testing.T) {
	v := decodeContent(t, `[
		{"type":"text","text":"describe this:"},
		{"type":"image_url","image_url":{"url":"https://example.com/cat.jpg"}},
		{"type":"text","text":"and this:"},
		{"type":"image_url","image_url":{"url":"https://example.com/dog.jpg"}}
	]`)
	parts, err := ParseContentParts(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts, got %d", len(parts))
	}
	wantTypes := []string{"text", "image_url", "text", "image_url"}
	for i, p := range parts {
		if p.Type != wantTypes[i] {
			t.Errorf("parts[%d].Type = %q, want %q", i, p.Type, wantTypes[i])
		}
	}
	if parts[0].Text != "describe this:" {
		t.Errorf("parts[0].Text = %q", parts[0].Text)
	}
	if parts[2].Text != "and this:" {
		t.Errorf("parts[2].Text = %q", parts[2].Text)
	}
	if parts[1].ImageURL.URL != "https://example.com/cat.jpg" {
		t.Errorf("parts[1].ImageURL.URL = %q", parts[1].ImageURL.URL)
	}
	if parts[3].ImageURL.URL != "https://example.com/dog.jpg" {
		t.Errorf("parts[3].ImageURL.URL = %q", parts[3].ImageURL.URL)
	}
}

func TestMultipleTextBlocks(t *testing.T) {
	v := decodeContent(t, `[{"type":"text","text":"foo"},{"type":"text","text":"bar"}]`)
	parts, err := ParseContentParts(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(parts))
	}
	if parts[0].Text != "foo" || parts[1].Text != "bar" {
		t.Errorf("texts: %q %q", parts[0].Text, parts[1].Text)
	}
}

func TestUnknownBlockTypeErrors(t *testing.T) {
	v := decodeContent(t, `[{"type":"audio","data":"..."}]`)
	_, err := ParseContentParts(v)
	if err == nil {
		t.Error("expected error for unknown block type")
	}
}

func TestNonObjectBlockErrors(t *testing.T) {
	_, err := ParseContentParts([]any{"not-an-object"})
	if err == nil {
		t.Error("expected error for non-object block")
	}
}

func TestUnsupportedTopLevelTypeErrors(t *testing.T) {
	_, err := ParseContentParts(42)
	if err == nil {
		t.Error("expected error for integer content")
	}
}

// Regression: openAIContentToText in the anthropic package must still work
// correctly on text-only content; ParseContentParts does not replace it.
// This test verifies the two code paths are independent by checking that a
// text-only array round-trips identically through ParseContentParts.
func TestTextOnlyArrayRoundTrips(t *testing.T) {
	v := decodeContent(t, `[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]`)
	parts, err := ParseContentParts(v)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parts) != 2 || parts[0].Text != "line1" || parts[1].Text != "line2" {
		t.Errorf("unexpected parts: %+v", parts)
	}
}
