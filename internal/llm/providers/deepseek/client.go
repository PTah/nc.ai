package deepseek

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"notcursor.ai/app/internal/llm"
)

const (
	DefaultBaseURL = "https://api.deepseek.com"
	DefaultModel   = "deepseek-v4-flash"
)

// Client talks to DeepSeek OpenAI-compatible Chat Completions API.
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

func New(apiKey, model string) *Client {
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: DefaultBaseURL,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *Client) Name() string { return "deepseek" }

type apiRequest struct {
	Model           string          `json:"model"`
	Messages        []llm.Message   `json:"messages"`
	Tools           []llm.ToolSpec  `json:"tools,omitempty"`
	ToolChoice      any             `json:"tool_choice,omitempty"`
	Stream          bool            `json:"stream"`
	Temperature     *float64        `json:"temperature,omitempty"`
	MaxTokens       *int            `json:"max_tokens,omitempty"`
	Thinking        map[string]any  `json:"thinking,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
}

func (c *Client) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("deepseek: api key is empty")
	}
	model := req.Model
	if model == "" {
		model = c.model
	}
	payload := apiRequest{
		Model:           model,
		Messages:        req.Messages,
		Tools:           req.Tools,
		ToolChoice:      req.ToolChoice,
		Stream:          false,
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		Thinking:        req.Thinking,
		ReasoningEffort: req.ReasoningEffort,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("deepseek: HTTP %d: %s", res.StatusCode, truncate(string(data), 500))
	}
	var out llm.ChatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ChatCompletionStream(ctx context.Context, req *llm.ChatRequest, out chan<- llm.StreamEvent) error {
	defer close(out)
	if c.apiKey == "" {
		out <- llm.StreamEvent{Type: "error", Content: "deepseek: api key is empty"}
		return fmt.Errorf("deepseek: api key is empty")
	}
	model := req.Model
	if model == "" {
		model = c.model
	}
	payload := apiRequest{
		Model:           model,
		Messages:        req.Messages,
		Tools:           req.Tools,
		ToolChoice:      req.ToolChoice,
		Stream:          true,
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		Thinking:        req.Thinking,
		ReasoningEffort: req.ReasoningEffort,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	res, err := c.http.Do(httpReq)
	if err != nil {
		out <- llm.StreamEvent{Type: "error", Content: err.Error()}
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		data, _ := io.ReadAll(res.Body)
		msg := fmt.Sprintf("deepseek: HTTP %d: %s", res.StatusCode, truncate(string(data), 500))
		out <- llm.StreamEvent{Type: "error", Content: msg}
		return fmt.Errorf("%s", msg)
	}

	reader := bufio.NewReader(res.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				out <- llm.StreamEvent{Type: "done"}
				return nil
			}
			out <- llm.StreamEvent{Type: "error", Content: err.Error()}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			out <- llm.StreamEvent{Type: "done"}
			return nil
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		choices, _ := chunk["choices"].([]any)
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		delta, _ := choice["delta"].(map[string]any)
		if content, ok := delta["content"].(string); ok && content != "" {
			out <- llm.StreamEvent{Type: "delta", Content: content, Raw: chunk}
		}
		if reasoning, ok := delta["reasoning_content"].(string); ok && reasoning != "" {
			out <- llm.StreamEvent{Type: "reasoning", Content: reasoning, Raw: chunk}
		}
		if fr, ok := choice["finish_reason"].(string); ok && fr != "" && fr != "null" {
			out <- llm.StreamEvent{Type: "done", Content: fr, Raw: chunk}
			return nil
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
