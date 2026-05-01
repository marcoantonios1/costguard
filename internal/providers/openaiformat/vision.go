package openaiformat

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Default image dimensions used when the actual dimensions cannot be determined
// (e.g., for HTTPS image URLs where fetching is not performed).
const defaultImageWidth = 1024
const defaultImageHeight = 1024

// Anthropic tile-based pricing constants.
// Reference: https://docs.anthropic.com/en/docs/build-with-claude/vision
const (
	anthropicMaxDim        = 1568
	anthropicTileSize      = 512
	anthropicTokensPerTile = 765
	anthropicBaseTokens    = 65
)

// OpenAI tile-based pricing constants.
// Reference: https://platform.openai.com/docs/guides/vision
const (
	openAIStep1MaxDim    = 2048
	openAIStep2ShortSide = 768
	openAITileSize       = 512
	openAITokensPerTile  = 170
	openAIBaseTokens     = 85
	openAILowDetail      = 85
)

// ExtractRequestImages parses an OpenAI-format request body and returns all
// image_url content blocks found across all messages.
func ExtractRequestImages(reqBody []byte) []ImageURL {
	var req struct {
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
	}
	if json.Unmarshal(reqBody, &req) != nil {
		return nil
	}

	var images []ImageURL
	for _, msg := range req.Messages {
		parts, err := ParseContentParts(msg.Content)
		if err != nil {
			continue
		}
		for _, p := range parts {
			if p.Type == "image_url" && p.ImageURL != nil {
				images = append(images, *p.ImageURL)
			}
		}
	}
	return images
}

// AnthropicImageTokens estimates the Anthropic token cost for a slice of images.
//
// Anthropic's formula: each image is resized to fit within 1568×1568 pixels
// (preserving aspect ratio), then divided into 512×512 tiles; the cost is
// tiles×765 + 65 per image.
//
// When the actual image dimensions cannot be determined (HTTPS URLs, unsupported
// formats), 1024×1024 is assumed as a conservative default.
func AnthropicImageTokens(images []ImageURL) int {
	total := 0
	for _, img := range images {
		w, h := imageDimensions(img.URL)
		total += anthropicTileTokens(w, h)
	}
	return total
}

// OpenAIImageTokens estimates the OpenAI token cost for a slice of images.
//
// For detail="low": flat 85 tokens per image.
// For detail="high", "auto", or unset (treated as high): scale to fit 2048×2048,
// scale the shortest side to 768, then count 512×512 tiles at 170 tokens each
// plus 85 base tokens.
//
// When dimensions are unknown, 1024×1024 is assumed.
func OpenAIImageTokens(images []ImageURL) int {
	total := 0
	for _, img := range images {
		if img.Detail == "low" {
			total += openAILowDetail
			continue
		}
		w, h := imageDimensions(img.URL)
		total += openAITileTokens(w, h)
	}
	return total
}

// imageDimensions returns the pixel dimensions of an image referenced by rawURL.
// For data URIs containing PNG images, the IHDR header is decoded to extract
// exact dimensions. All other formats (JPEG data URIs, HTTPS URLs, etc.) fall
// back to the 1024×1024 default.
func imageDimensions(rawURL string) (int, int) {
	if !strings.HasPrefix(rawURL, "data:") {
		return defaultImageWidth, defaultImageHeight
	}

	commaIdx := strings.IndexByte(rawURL, ',')
	if commaIdx < 0 {
		return defaultImageWidth, defaultImageHeight
	}

	header := rawURL[:commaIdx]
	b64Data := rawURL[commaIdx+1:]

	if strings.Contains(header, "image/png") {
		if w, h, ok := pngDimensionsFromBase64(b64Data); ok {
			return w, h
		}
	}
	// JPEG SOF scanning is omitted; fall through to default.
	return defaultImageWidth, defaultImageHeight
}

// pngDimensionsFromBase64 extracts image width and height from the IHDR chunk
// of a base64-encoded PNG. Only the first 32 base64 characters (24 raw bytes)
// are decoded, so this is O(1) with no heap allocation for large images.
func pngDimensionsFromBase64(b64 string) (int, int, bool) {
	// 24 raw bytes = 32 base64 chars (24 ÷ 3 × 4, no padding needed).
	if len(b64) < 32 {
		return 0, 0, false
	}
	raw, err := base64.StdEncoding.DecodeString(b64[:32])
	if err != nil {
		// Try URL-safe encoding (some clients use it).
		raw, err = base64.URLEncoding.DecodeString(b64[:32])
		if err != nil {
			return 0, 0, false
		}
	}
	if len(raw) < 24 {
		return 0, 0, false
	}
	// Verify the PNG signature at bytes 0-7: \x89PNG\r\n\x1a\n
	if raw[0] != 0x89 || raw[1] != 'P' || raw[2] != 'N' || raw[3] != 'G' {
		return 0, 0, false
	}
	// Width at bytes 16-19, height at bytes 20-23 (big-endian uint32).
	w := int(raw[16])<<24 | int(raw[17])<<16 | int(raw[18])<<8 | int(raw[19])
	h := int(raw[20])<<24 | int(raw[21])<<16 | int(raw[22])<<8 | int(raw[23])
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

// anthropicTileTokens computes Anthropic token cost for an image of given dimensions.
func anthropicTileTokens(width, height int) int {
	w, h := anthropicResize(width, height)
	tilesW := (w + anthropicTileSize - 1) / anthropicTileSize
	tilesH := (h + anthropicTileSize - 1) / anthropicTileSize
	return tilesW*tilesH*anthropicTokensPerTile + anthropicBaseTokens
}

// anthropicResize scales width×height to fit within 1568×1568, preserving
// aspect ratio. Images already within the bound are returned unchanged.
func anthropicResize(width, height int) (int, int) {
	if width <= anthropicMaxDim && height <= anthropicMaxDim {
		return width, height
	}
	if width >= height {
		return anthropicMaxDim, height * anthropicMaxDim / width
	}
	return width * anthropicMaxDim / height, anthropicMaxDim
}

// openAITileTokens computes OpenAI high-detail token cost for an image.
func openAITileTokens(width, height int) int {
	w, h := openAIResize(width, height)
	tilesW := (w + openAITileSize - 1) / openAITileSize
	tilesH := (h + openAITileSize - 1) / openAITileSize
	return tilesW*tilesH*openAITokensPerTile + openAIBaseTokens
}

// openAIResize applies OpenAI's two-step scaling for high-detail images:
// 1. Fit within 2048×2048 (preserving aspect ratio).
// 2. Scale the shortest side to 768 (preserving aspect ratio).
func openAIResize(width, height int) (int, int) {
	w, h := width, height

	// Step 1: fit within 2048×2048.
	if w > openAIStep1MaxDim || h > openAIStep1MaxDim {
		if w >= h {
			h = h * openAIStep1MaxDim / w
			w = openAIStep1MaxDim
		} else {
			w = w * openAIStep1MaxDim / h
			h = openAIStep1MaxDim
		}
	}

	// Step 2: scale shortest side to 768.
	if w <= h {
		if w > openAIStep2ShortSide {
			h = h * openAIStep2ShortSide / w
			w = openAIStep2ShortSide
		}
	} else {
		if h > openAIStep2ShortSide {
			w = w * openAIStep2ShortSide / h
			h = openAIStep2ShortSide
		}
	}

	return w, h
}
