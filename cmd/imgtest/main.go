// Manual smoke-test for the Anthropic image transform.
// Usage: go run ./cmd/imgtest [image-url]
// Defaults to a small public PNG if no URL is given.
// Reads ANTHROPIC_API_KEY from the environment (or .env in the project root).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/marcoantonios1/costguard/internal/providers/anthropic"
)

func main() {
	_ = godotenv.Load(".env")

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "ANTHROPIC_API_KEY is not set")
		os.Exit(1)
	}

	imageURL := "https://commons.wikimedia.org/wiki/Main_Page#/media/File:Ashy_Prinia_in_Ajodhya_Hills_July_2024_by_Tisha_Mukherjee_01.jpg"
	if len(os.Args) > 1 {
		imageURL = os.Args[1]
	}

	payload := map[string]any{
		"model": "claude-sonnet-4-6",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "Describe this image in one sentence."},
					map[string]any{
						"type":      "image_url",
						"image_url": map[string]any{"url": imageURL},
					},
				},
			},
		},
	}

	body, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Println("=== OpenAI-format request ===")
	fmt.Println(string(body))

	client, err := anthropic.NewClient(anthropic.ClientConfig{
		Name:   "test",
		APIKey: apiKey,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "new client:", err)
		os.Exit(1)
	}

	u, _ := url.Parse("http://localhost/v1/chat/completions")
	req, _ := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		u.String(),
		io.NopCloser(bytes.NewReader(body)),
	)
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(context.Background(), req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "do:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Printf("\n=== Response (status %d, %.2fs) ===\n", resp.StatusCode, time.Since(start).Seconds())

	var pretty any
	if json.Unmarshal(respBody, &pretty) == nil {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Println(string(respBody))
	}
}
