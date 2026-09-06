// Command checkconnect verifies DeepSeek connectivity via the app's provider client.
// Usage: DEEPSEEK_API_KEY=... go run ./cmd/checkconnect
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"notcursor.ai/app/internal/llm"
	"notcursor.ai/app/internal/llm/providers/deepseek"
)

func main() {
	key := os.Getenv("DEEPSEEK_API_KEY")
	if key == "" {
		fmt.Println("CONNECT_OK=false")
		fmt.Println("ERROR=DEEPSEEK_API_KEY is empty")
		os.Exit(1)
	}

	client := deepseek.New(key, deepseek.DefaultModel)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	maxTokens := 16
	resp, err := client.ChatCompletion(ctx, &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "ping"},
		},
		MaxTokens: &maxTokens,
		Thinking:  map[string]any{"type": "disabled"},
	})
	if err != nil {
		fmt.Println("CONNECT_OK=false")
		fmt.Printf("ERROR=%v\n", err)
		os.Exit(1)
	}

	fmt.Println("CONNECT_OK=true")
	fmt.Printf("PROVIDER=%s\n", client.Name())
	fmt.Printf("MODEL=%s\n", resp.Model)
	if len(resp.Choices) > 0 {
		fmt.Printf("FINISH=%s\n", resp.Choices[0].FinishReason)
	}
	fmt.Printf("CHOICES=%d\n", len(resp.Choices))
}
