// Manual smoke-test for image transforms.
// Usage: go run ./cmd/imgtest [--provider anthropic|gemini] [image-url-or-data-uri]
// Reads API keys from .env in the project root.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/marcoantonios1/costguard/internal/providers/anthropic"
	"github.com/marcoantonios1/costguard/internal/providers/gemini"
)

type doer interface {
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
}

func main() {
	_ = godotenv.Load(".env")

	provider := flag.String("provider", "anthropic", "Provider to test: anthropic or gemini")
	flag.Parse()

	imageURL := flag.Arg(0)
	if imageURL == "" {
		imageURL = "https://upload.wikimedia.org/wikipedia/commons/thumb/4/47/PNG_transparency_demonstration_1.png/240px-PNG_transparency_demonstration_1.png"
	}

	var client doer
	var model string

	switch *provider {
	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			fmt.Fprintln(os.Stderr, "ANTHROPIC_API_KEY is not set")
			os.Exit(1)
		}
		c, err := anthropic.NewClient(anthropic.ClientConfig{Name: "test", APIKey: apiKey})
		if err != nil {
			fmt.Fprintln(os.Stderr, "new anthropic client:", err)
			os.Exit(1)
		}
		client = c
		model = "claude-sonnet-4-6"

	case "gemini":
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			fmt.Fprintln(os.Stderr, "GEMINI_API_KEY is not set")
			os.Exit(1)
		}
		c, err := gemini.NewClient(gemini.ClientConfig{Name: "test", APIKey: apiKey})
		if err != nil {
			fmt.Fprintln(os.Stderr, "new gemini client:", err)
			os.Exit(1)
		}
		client = c
		model = "gemini-2.5-flash"

	default:
		fmt.Fprintf(os.Stderr, "unknown provider %q; use anthropic or gemini\n", *provider)
		os.Exit(1)
	}

	payload := map[string]any{
		"model": model,
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
	fmt.Printf("=== OpenAI-format request (provider: %s) ===\n", *provider)
	fmt.Println(string(body))

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
