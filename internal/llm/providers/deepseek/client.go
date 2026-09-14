package deepseek

import (
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
	VisionModel    = "deepseek-v4-flash-vision-exp"
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
		if err := llm.ValidateToolHistory("deepseek", req.Messages); err != nil {
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
		return nil, mapAPIError(res.StatusCode, data)
	}
	var out llm.ChatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("deepseek: decode: %w", err)
	}
	if out.Model == "" {
		out.Model = model
	}
	return &out, nil
}

type apiErrorBody struct {
	Error struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// mapAPIError turns DeepSeek JSON error payloads into short user-facing messages.
func mapAPIError(status int, body []byte) error {
	var payload apiErrorBody
	_ = json.Unmarshal(body, &payload)
	msg := strings.TrimSpace(payload.Error.Message)
	switch status {
	case 402:
		return fmt.Errorf("Недостаточно баланса DeepSeek. Пополните счёт и нажмите Reconnect.")
	case 401:
		return fmt.Errorf("DeepSeek: неверный API-ключ.")
	case 403:
		if strings.Contains(strings.ToLower(msg), "balance") {
			return fmt.Errorf("Недостаточно баланса DeepSeek. Пополните счёт и нажмите Reconnect.")
		}
		return fmt.Errorf("DeepSeek: доступ запрещён (403).")
	case 429:
		return fmt.Errorf("Превышен лимит запросов DeepSeek, попробуйте позднее…")
	case 500, 502, 503:
		return fmt.Errorf("DeepSeek временно недоступен (%d), попробуйте позднее…", status)
	}
	detail := msg
	if detail == "" {
		detail = llm.TruncateRunes(string(body), 400)
	}
	return fmt.Errorf("deepseek: HTTP %d: %s", status, detail)
}

func thinkingEnabled(thinking map[string]any) bool {
	if thinking == nil {
		return true
	}
	t, _ := thinking["type"].(string)
	return t != "disabled"
}

// ModelInfo is one entry from GET /models.
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type modelsListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ListModels calls GET /models (OpenAI-compatible catalog for the API key).
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("deepseek: api key is empty")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
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
		return nil, mapAPIError(res.StatusCode, data)
	}
	var out modelsListResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("deepseek: decode models: %w", err)
	}
	return out.Data, nil
}

// FallbackModels is used when /models is unavailable (no key / network).
func FallbackModels() []string {
	return []string{
		DefaultModel,
		"deepseek-v4-pro",
		VisionModel,
	}
}

// MergeKnownModels ensures fallback ids stay listed even if GET /models omits them
// (e.g. experimental vision).
func MergeKnownModels(available []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(available)+3)
	for _, id := range available {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, known := range FallbackModels() {
		if !seen[known] {
			seen[known] = true
			out = append(out, known)
		}
	}
	return out
}

// PreferModel keeps current when still listed; else DefaultModel; else first id.
func PreferModel(available []string, current string) string {
	current = strings.TrimSpace(current)
	if len(available) == 0 {
		if current != "" {
			return current
		}
		return DefaultModel
	}
	seen := map[string]bool{}
	for _, id := range available {
		seen[id] = true
	}
	if current != "" && seen[current] {
		return current
	}
	if seen[DefaultModel] {
		return DefaultModel
	}
	return available[0]
}

// OrderModels puts flash, then pro, then vision, then the rest (stable).
func OrderModels(available []string) []string {
	if len(available) == 0 {
		return nil
	}
	priority := []string{DefaultModel, "deepseek-v4-pro", VisionModel}
	seen := map[string]bool{}
	out := make([]string, 0, len(available))
	for _, want := range priority {
		for _, id := range available {
			if id == want && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	for _, id := range available {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

