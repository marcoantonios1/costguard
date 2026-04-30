package openaiformat

import "fmt"

// ContentPart represents a single block inside an OpenAI message content array.
// A content field may be a plain string (treated as a single text part) or an
// array of typed blocks — currently "text" and "image_url" are supported.
type ContentPart struct {
	Type     string   // "text" | "image_url"
	Text     string   // populated when Type == "text"
	ImageURL *ImageURL // populated when Type == "image_url"
}

// ImageURL holds the payload of an OpenAI image_url content block.
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto" | "low" | "high"
}

// ParseContentParts converts the OpenAI wire-format content field into a typed
// slice of ContentParts.  v may be:
//   - string   → single text part
//   - []any    → array of {type:"text",...} or {type:"image_url",...} blocks;
//     order is preserved
//   - nil      → empty slice, no error
//
// Unknown top-level types or unrecognised block types return an error.
func ParseContentParts(v any) ([]ContentPart, error) {
	switch c := v.(type) {
	case string:
		return []ContentPart{{Type: "text", Text: c}}, nil

	case []any:
		parts := make([]ContentPart, 0, len(c))
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("content block is not an object")
			}
			t, _ := m["type"].(string)
			switch t {
			case "text":
				text, _ := m["text"].(string)
				parts = append(parts, ContentPart{Type: "text", Text: text})

			case "image_url":
				iu, _ := m["image_url"].(map[string]any)
				url, _ := iu["url"].(string)
				detail, _ := iu["detail"].(string)
				parts = append(parts, ContentPart{
					Type:     "image_url",
					ImageURL: &ImageURL{URL: url, Detail: detail},
				})

			default:
				return nil, fmt.Errorf("unsupported content block type: %q", t)
			}
		}
		return parts, nil

	case nil:
		return nil, nil

	default:
		return nil, fmt.Errorf("unsupported content format")
	}
}
