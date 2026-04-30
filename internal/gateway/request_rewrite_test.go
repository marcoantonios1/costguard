package gateway

import (
	"encoding/json"
	"testing"
)

func TestRewriteModelPreservesImageURLBlock(t *testing.T) {
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/photo.png"}}]}]}`)

	result, err := rewriteModelInBody(body, "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if out["model"] != "gpt-4o" {
		t.Errorf("model: got %v, want gpt-4o", out["model"])
	}

	imageURL := imageURLField(t, out)
	if imageURL["url"] != "https://example.com/photo.png" {
		t.Errorf("url: got %v", imageURL["url"])
	}
}

func TestRewriteModelPreservesDetailField(t *testing.T) {
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/photo.png","detail":"high"}}]}]}`)

	result, err := rewriteModelInBody(body, "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	imageURL := imageURLField(t, out)
	if imageURL["detail"] != "high" {
		t.Errorf("detail: got %v, want high", imageURL["detail"])
	}
}

func TestRewriteModelPreservesImageOnlyMessage(t *testing.T) {
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/photo.png"}}]}]}`)

	result, err := rewriteModelInBody(body, "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	messages := out["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}
	block := content[0].(map[string]any)
	if block["type"] != "image_url" {
		t.Errorf("type: got %v, want image_url", block["type"])
	}
}

func TestRewriteModelPreservesMixedTextAndImageBlocks(t *testing.T) {
	body := []byte(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"https://example.com/a.png","detail":"low"}}]}]}`)

	result, err := rewriteModelInBody(body, "gpt-4o")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	messages := out["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}

	textBlock := content[0].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "describe this" {
		t.Errorf("text block: %+v", textBlock)
	}

	imageBlock := content[1].(map[string]any)
	imageURL := imageBlock["image_url"].(map[string]any)
	if imageURL["detail"] != "low" {
		t.Errorf("detail: got %v, want low", imageURL["detail"])
	}
}

// imageURLField navigates messages[0].content[0].image_url in the parsed body.
func imageURLField(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	messages := body["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	block := content[0].(map[string]any)
	return block["image_url"].(map[string]any)
}
