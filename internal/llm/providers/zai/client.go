package zai

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
	// PaasBaseURL is pay-as-you-go (OpenAI SDK quick-start) — default for NotCursor.
	PaasBaseURL = "https://api.z.ai/api/paas/v4"
	// CodingBaseURL is for GLM Coding Plan / subscription coding agents.
	// See https://docs.z.ai/devpack/latest-model
	CodingBaseURL = "https://api.z.ai/api/coding/paas/v4"
	// DefaultBaseURL prefers pay-as-you-go (most common for this app).
	DefaultBaseURL = PaasBaseURL
	DefaultModel   = "glm-4.7-flash"

	EndpointCoding = "coding"
	EndpointPaas   = "paas"
)

// FreeModels are $0 on Z.ai pay-as-you-go (preference order for auto-pick).
var FreeModels = []string{
	"glm-4.7-flash",
	"glm-4.5-flash",
}

// defaultTemperature matches Z.ai coding guidance (low temp for code).
var defaultTemperature = 0.2

// BaseURLFor returns the API base for a stored endpoint id.
func BaseURLFor(endpoint string) string {
	switch endpoint {
	case EndpointCoding:
		return CodingBaseURL
	default:
		return PaasBaseURL
	}
}

// Client talks to Z.ai / BigModel OpenAI-compatible Chat Completions API.
// Wire format follows https://docs.z.ai/ (thinking + tool calls).
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

func New(apiKey, model string) *Client {
	return NewWithBaseURL(apiKey, model, DefaultBaseURL)
}

func NewWithBaseURL(apiKey, model, baseURL string) *Client {
	if model == "" {
		model = DefaultModel
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

func (c *Client) Name() string { return "zai" }

func (c *Client) SetAPIKey(key string) { c.apiKey = key }
func (c *Client) SetModel(model string) {
	if model == "" {
		model = DefaultModel
	}
	c.model = model
}
func (c *Client) SetBaseURL(baseURL string) {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	c.baseURL = baseURL
}

// ModelInfo is one entry from GET /models (OpenAI-compatible).
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type modelsListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ListModels calls GET {base}/models and returns model ids available to the key.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("zai: api key is empty")
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
		return nil, fmt.Errorf("zai: HTTP %d: %s", res.StatusCode, truncate(string(data), 800))
	}
	var out modelsListResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("zai: decode models: %w", err)
	}
	return out.Data, nil
}

// FallbackModels is used when /models is unavailable (no key / network).
func FallbackModels() []string {
	return []string{
		"glm-4.7-flash",
		"glm-4.5-flash",
		"glm-5.3",
		"glm-5.3-flash",
		"glm-5.2",
		"glm-5.1",
		"glm-5",
		"glm-4.7",
		"glm-4.6",
		"glm-4.5",
	}
}

// PreferModel picks a model from available ids: keep current if still listed,
// else first free-tier id, else the first available id.
func PreferModel(available []string, current string) string {
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
	for _, free := range FreeModels {
		if seen[free] {
			return free
		}
	}
	return available[0]
}

// OrderModels puts free-tier ids first, then the rest (stable).
func OrderModels(available []string) []string {
	if len(available) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(available))
	for _, free := range FreeModels {
		for _, id := range available {
			if id == free && !seen[id] {
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
		return nil, fmt.Errorf("zai: api key is empty")
	}
	if req == nil {
		return nil, fmt.Errorf("zai: nil request")
	}
	model := req.Model
	if model == "" {
		model = c.model
	}

	thinking := req.Thinking
	effort := req.ReasoningEffort
	hasTools := len(req.Tools) > 0
	if hasTools {
		if err := validateToolHistory(req.Messages); err != nil {
			return nil, err
		}
		thinking = map[string]any{"type": "enabled"}
		if effort == "" {
			effort = "high"
		}
	} else if thinkingDisabled(thinking) {
		// GLM-5.3+ always thinks — cannot send type=disabled (API 1210).
		// Map "disable" requests to the lightest allowed effort.
		thinking = map[string]any{"type": "enabled"}
		if effort == "" {
			effort = "low"
		}
	} else {
		if thinking == nil {
			thinking = map[string]any{"type": "enabled"}
		}
		if effort == "" {
			effort = "high"
		}
	}

	temp := req.Temperature
	if temp == nil {
		temp = &defaultTemperature
	}

	payload := apiRequest{
		Model:           model,
		Messages:        req.Messages,
		Tools:           req.Tools,
		ToolChoice:      req.ToolChoice,
		Stream:          false,
		MaxTokens:       req.MaxTokens,
		Thinking:        thinking,
		ReasoningEffort: effort,
		Temperature:     temp,
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
		return nil, fmt.Errorf("zai: HTTP %d: %s", res.StatusCode, truncate(string(data), 800))
	}
	var out llm.ChatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("zai: decode: %w", err)
	}
	if out.Model == "" {
		out.Model = model
	}
	return &out, nil
}

func validateToolHistory(messages []llm.Message) error {
	known := map[string]bool{}
	for i, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == "" {
				return fmt.Errorf("zai: assistant tool_call missing id (index %d)", i)
			}
			if tc.Function.Name == "" {
				return fmt.Errorf("zai: assistant tool_call missing function.name (index %d)", i)
			}
			known[tc.ID] = true
		}
	}
	for i, m := range messages {
		if m.Role != "tool" {
			continue
		}
		if m.ToolCallID == "" {
			return fmt.Errorf("zai: tool message missing tool_call_id (index %d)", i)
		}
		if !known[m.ToolCallID] {
			return fmt.Errorf("zai: tool message references unknown tool_call_id %q (index %d)", m.ToolCallID, i)
		}
	}
	return nil
}

func thinkingDisabled(thinking map[string]any) bool {
	if thinking == nil {
		return false
	}
	t, _ := thinking["type"].(string)
	return t == "disabled"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
