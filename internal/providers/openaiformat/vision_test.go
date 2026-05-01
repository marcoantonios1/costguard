package openaiformat

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makePNGHeader builds 24 bytes that look like a valid PNG IHDR header for
// the given dimensions, then returns the full base64-encoded string.
// The bytes beyond the 24-byte header are zero-padded so the base64 string
// is a multiple of 4 characters (no padding issues when slicing).
func makePNGHeader(width, height int) string {
	raw := make([]byte, 24)
	// PNG signature: \x89PNG\r\n\x1a\n
	raw[0], raw[1], raw[2], raw[3] = 0x89, 'P', 'N', 'G'
	raw[4], raw[5], raw[6], raw[7] = '\r', '\n', 0x1a, '\n'
	// IHDR length (big-endian uint32 = 13)
	raw[8], raw[9], raw[10], raw[11] = 0, 0, 0, 13
	// "IHDR" chunk type
	raw[12], raw[13], raw[14], raw[15] = 'I', 'H', 'D', 'R'
	// Width (big-endian uint32)
	raw[16] = byte(width >> 24)
	raw[17] = byte(width >> 16)
	raw[18] = byte(width >> 8)
	raw[19] = byte(width)
	// Height (big-endian uint32)
	raw[20] = byte(height >> 24)
	raw[21] = byte(height >> 16)
	raw[22] = byte(height >> 8)
	raw[23] = byte(height)
	return base64.StdEncoding.EncodeToString(raw)
}

// ---------------------------------------------------------------------------
// pngDimensionsFromBase64
// ---------------------------------------------------------------------------

func TestPNGDimensionsFromBase64_ValidHeader(t *testing.T) {
	tests := []struct{ w, h int }{
		{512, 512},
		{1024, 768},
		{1920, 1080},
		{100, 200},
	}
	for _, tc := range tests {
		b64 := makePNGHeader(tc.w, tc.h)
		gotW, gotH, ok := pngDimensionsFromBase64(b64)
		if !ok {
			t.Errorf("%dx%d: expected ok=true", tc.w, tc.h)
		}
		if gotW != tc.w || gotH != tc.h {
			t.Errorf("%dx%d: got %dx%d", tc.w, tc.h, gotW, gotH)
		}
	}
}

func TestPNGDimensionsFromBase64_TooShort(t *testing.T) {
	_, _, ok := pngDimensionsFromBase64("abc")
	if ok {
		t.Error("expected ok=false for too-short input")
	}
}

func TestPNGDimensionsFromBase64_NotPNG(t *testing.T) {
	raw := make([]byte, 24) // all zeros — not a valid PNG signature
	b64 := base64.StdEncoding.EncodeToString(raw)
	_, _, ok := pngDimensionsFromBase64(b64)
	if ok {
		t.Error("expected ok=false for non-PNG data")
	}
}

// ---------------------------------------------------------------------------
// Anthropic resize + tile formula
// ---------------------------------------------------------------------------

func TestAnthropicResize_NoChange(t *testing.T) {
	w, h := anthropicResize(512, 512)
	if w != 512 || h != 512 {
		t.Errorf("got %dx%d, want 512x512", w, h)
	}
}

func TestAnthropicResize_WideImage(t *testing.T) {
	// 3136×800 — width exceeds 1568 → scale down.
	w, h := anthropicResize(3136, 800)
	if w != 1568 {
		t.Errorf("width: got %d, want 1568", w)
	}
	if h > anthropicMaxDim {
		t.Errorf("height %d exceeds maxDim", h)
	}
}

func TestAnthropicTileTokens_512x512(t *testing.T) {
	// 512×512 → 1×1 tile → 1×765+65 = 830
	got := anthropicTileTokens(512, 512)
	if got != 830 {
		t.Errorf("got %d, want 830", got)
	}
}

func TestAnthropicTileTokens_1024x1024(t *testing.T) {
	// 1024×1024 → 2×2 tiles → 4×765+65 = 3125
	got := anthropicTileTokens(1024, 1024)
	if got != 3125 {
		t.Errorf("got %d, want 3125", got)
	}
}

func TestAnthropicTileTokens_1568x1568(t *testing.T) {
	// 1568×1568 → already at max → ceil(1568/512)=4 → 4×4=16 tiles → 16×765+65=12305
	got := anthropicTileTokens(1568, 1568)
	if got != 12305 {
		t.Errorf("got %d, want 12305", got)
	}
}

// ---------------------------------------------------------------------------
// OpenAI resize + tile formula
// ---------------------------------------------------------------------------

func TestOpenAIResize_FitsWithin2048(t *testing.T) {
	// 1024×1024 — fits within 2048×2048; shortest side 1024 > 768 → scale to 768.
	w, h := openAIResize(1024, 1024)
	if w != 768 || h != 768 {
		t.Errorf("got %dx%d, want 768x768", w, h)
	}
}

func TestOpenAIResize_LargeImage(t *testing.T) {
	// 4096×4096 → step1: scale to 2048×2048 → step2: 2048 > 768 → 768×768
	w, h := openAIResize(4096, 4096)
	if w != 768 || h != 768 {
		t.Errorf("got %dx%d, want 768x768", w, h)
	}
}

func TestOpenAIResize_SmallImage_NoChange(t *testing.T) {
	// 300×200 — fits within 2048; shortest side 200 < 768 → no change.
	w, h := openAIResize(300, 200)
	if w != 300 || h != 200 {
		t.Errorf("got %dx%d, want 300x200", w, h)
	}
}

func TestOpenAITileTokens_1024x1024(t *testing.T) {
	// 1024×1024 → resize → 768×768 → ceil(768/512)=2 → 2×2=4 tiles → 4×170+85 = 765
	got := openAITileTokens(1024, 1024)
	if got != 765 {
		t.Errorf("got %d, want 765", got)
	}
}

func TestOpenAITileTokens_512x512(t *testing.T) {
	// 512×512 → resize: fits 2048, shortest side 512 < 768 no scale → 1×1=1 tile → 1×170+85 = 255
	got := openAITileTokens(512, 512)
	if got != 255 {
		t.Errorf("got %d, want 255", got)
	}
}

// ---------------------------------------------------------------------------
// AnthropicImageTokens (end-to-end with default dimensions)
// ---------------------------------------------------------------------------

func TestAnthropicImageTokens_HTTPSUrl(t *testing.T) {
	images := []ImageURL{{URL: "https://example.com/photo.png"}}
	got := AnthropicImageTokens(images)
	// Default 1024×1024 → 4 tiles → 4×765+65 = 3125
	if got != 3125 {
		t.Errorf("got %d, want 3125", got)
	}
}

func TestAnthropicImageTokens_DataURIPNG512(t *testing.T) {
	b64 := makePNGHeader(512, 512)
	images := []ImageURL{{URL: "data:image/png;base64," + b64}}
	got := AnthropicImageTokens(images)
	// 512×512 → 1 tile → 830
	if got != 830 {
		t.Errorf("got %d, want 830", got)
	}
}

func TestAnthropicImageTokens_MultipleImages(t *testing.T) {
	images := []ImageURL{
		{URL: "https://example.com/a.png"},
		{URL: "https://example.com/b.png"},
	}
	got := AnthropicImageTokens(images)
	// 2 × 3125 = 6250
	if got != 6250 {
		t.Errorf("got %d, want 6250", got)
	}
}

// ---------------------------------------------------------------------------
// OpenAIImageTokens
// ---------------------------------------------------------------------------

func TestOpenAIImageTokens_LowDetail(t *testing.T) {
	images := []ImageURL{{URL: "https://example.com/photo.png", Detail: "low"}}
	got := OpenAIImageTokens(images)
	if got != 85 {
		t.Errorf("got %d, want 85", got)
	}
}

func TestOpenAIImageTokens_HighDetail_DefaultDimensions(t *testing.T) {
	images := []ImageURL{{URL: "https://example.com/photo.png", Detail: "high"}}
	got := OpenAIImageTokens(images)
	// 1024×1024 → 768×768 → 4 tiles → 765
	if got != 765 {
		t.Errorf("got %d, want 765", got)
	}
}

func TestOpenAIImageTokens_AutoDetail_SameAsHigh(t *testing.T) {
	high := OpenAIImageTokens([]ImageURL{{URL: "https://example.com/x.png", Detail: "high"}})
	auto := OpenAIImageTokens([]ImageURL{{URL: "https://example.com/x.png", Detail: "auto"}})
	none := OpenAIImageTokens([]ImageURL{{URL: "https://example.com/x.png"}})
	if high != auto || high != none {
		t.Errorf("high=%d auto=%d none=%d — should all be equal", high, auto, none)
	}
}

// ---------------------------------------------------------------------------
// ExtractRequestImages
// ---------------------------------------------------------------------------

func TestExtractRequestImages_StringContent(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	})
	got := ExtractRequestImages(body)
	if len(got) != 0 {
		t.Errorf("expected 0 images, got %d", len(got))
	}
}

func TestExtractRequestImages_OneImage(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.png"}},
				},
			},
		},
	})
	got := ExtractRequestImages(body)
	if len(got) != 1 {
		t.Fatalf("expected 1 image, got %d", len(got))
	}
	if got[0].URL != "https://example.com/cat.png" {
		t.Errorf("url: got %q", got[0].URL)
	}
}

func TestExtractRequestImages_DetailPreserved(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": "https://example.com/x.png", "detail": "low"},
					},
				},
			},
		},
	})
	got := ExtractRequestImages(body)
	if len(got) != 1 {
		t.Fatalf("expected 1 image, got %d", len(got))
	}
	if got[0].Detail != "low" {
		t.Errorf("detail: got %q, want low", got[0].Detail)
	}
}

func TestExtractRequestImages_MultipleMessagesAndImages(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "describe"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://a.com/1.png"}},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://a.com/2.png"}},
				},
			},
		},
	})
	got := ExtractRequestImages(body)
	if len(got) != 2 {
		t.Fatalf("expected 2 images, got %d", len(got))
	}
}

func TestExtractRequestImages_InvalidJSON(t *testing.T) {
	got := ExtractRequestImages([]byte("not json"))
	if got != nil {
		t.Errorf("expected nil for invalid JSON, got %v", got)
	}
}
