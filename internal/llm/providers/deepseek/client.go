package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"notcursor.ai/app/internal/llm"
)

const (
	DefaultBaseURL = "https://api.deepseek.com"
	DefaultModel   = "deepseek-v4-flash"
)

// Client talks to DeepSeek OpenAI-compatible Chat Completions API.
// Wire format follows https://api-docs.deepseek.com/ (thinking + tool calls).
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
			Timeout: 180 * time.Second,
		},
	}
}

func (c *Client) Name() string { return "deepseek" }

func (c *Client) SetAPIKey(key string) { c.apiKey = key }
func (c *Client) SetModel(model string) {
	if model == "" {
		model = DefaultModel
	}
	c.model = model
}

type apiRequest struct {
	Model           string         `json:"model"`
	Messages        []llm.Message  `json:"messages"`
	Tools           []llm.ToolSpec `json:"tools,omitempty"`
	ToolChoice      any            `json:"tool_choice,omitempty"`
	Stream          bool           `json:"stream"`
	Temperature     *float64       `json:"temperature,omitempty"`
	MaxTokens       *int           `json:"max_tokens,omitempty"`
	Thinking        map[string]any `json:"thinking,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
}

func (c *Client) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("deepseek: api key is empty")
	}
	if req == nil {
		return nil, fmt.Errorf("deepseek: nil request")
	}
	model := req.Model
	if model == "" {
		model = c.model
	}

	// Protocol (Thinking Mode + Tool Calls):
	// - thinking enabled by default; agent/tool requests MUST keep it enabled
	// - when tools are present, reasoning_content of prior assistants must round-trip
	// - temperature/top_p are ignored in thinking mode — do not send them
	thinking := req.Thinking
	hasTools := len(req.Tools) > 0
	if hasTools {
		thinking = map[string]any{"type": "enabled"}
	} else if thinking == nil {
		thinking = map[string]any{"type": "enabled"}
	}
	effort := req.ReasoningEffort
	if effort == "" && thinkingEnabled(thinking) {
		effort = "high"
	}
	if hasTools {
		if err := validateToolHistory(req.Messages); err != nil {
			return nil, err
		}
	}

	payload := apiRequest{
		Model:           model,
		Messages:        req.Messages,
		Tools:           req.Tools,
		ToolChoice:      req.ToolChoice,
		Stream:          false, // stage-1: non-stream only (SSE later)
		MaxTokens:       req.MaxTokens,
		Thinking:        thinking,
		ReasoningEffort: effort,
	}
	if !thinkingEnabled(thinking) {
		payload.Temperature = req.Temperature
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
	httpReq.Header.Set("Accept", "application/json")

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
		return nil, fmt.Errorf("deepseek: HTTP %d: %s", res.StatusCode, truncate(string(data), 800))
	}
	var out llm.ChatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("deepseek: decode: %w", err)
	}
	return &out, nil
}

// validateToolHistory mirrors DeepSeek rule: with tools present, prior assistant
// messages that had reasoning_content must still carry it (append message as-is).
func validateToolHistory(messages []llm.Message) error {
	for i, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		// Soft check: tool results must reference a prior tool_call_id when present.
		_ = i
		if len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					return fmt.Errorf("deepseek: assistant tool_call missing id (index %d)", i)
				}
				if tc.Function.Name == "" {
					return fmt.Errorf("deepseek: assistant tool_call missing function.name (index %d)", i)
				}
			}
		}
	}
	for i, m := range messages {
		if m.Role != "tool" {
			continue
		}
		if m.ToolCallID == "" {
			return fmt.Errorf("deepseek: tool message missing tool_call_id (index %d)", i)
		}
	}
	return nil
}

func thinkingEnabled(thinking map[string]any) bool {
	if thinking == nil {
		return true
	}
	t, _ := thinking["type"].(string)
	return t != "disabled"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
