package zai

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
		return nil, mapAPIError(res.StatusCode, data)
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

// IsFreeModel reports whether id is an official $0 pay-as-you-go model.
// Source: https://docs.z.ai/guides/overview/pricing
func IsFreeModel(id string) bool {
	for _, free := range FreeModels {
		if id == free {
			return true
		}
	}
	return false
}

// MergeFreeModels ensures official free ids are present even if GET /models omitted them.
func MergeFreeModels(available []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(available)+len(FreeModels))
	for _, id := range available {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, free := range FreeModels {
		if !seen[free] {
			seen[free] = true
			out = append(out, free)
		}
	}
	return out
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

// PreferFreeModel is an alias of PreferModel (kept for older call sites/tests).
// Manual user choice is always preserved when the model remains in `available`.
func PreferFreeModel(available []string, current string) string {
	return PreferModel(available, current)
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
		Messages:        prepareMessages(model, req.Messages),
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
		return nil, mapAPIError(res.StatusCode, data)
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

// supportsVision reports models that accept image_url content parts.
// glm-4.7-flash / glm-4.5-flash / glm-5.3 are text-only; glm-5.3-flash and *v* are multimodal.
func supportsVision(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "5.3-flash"):
		return true
	case strings.Contains(m, "4.6v"), strings.Contains(m, "4.5v"), strings.Contains(m, "5v"):
		return true
	default:
		return false
	}
}

// prepareMessages adapts history for Z.ai: text-only models cannot receive image_url
// (API 1210: messages.content.type allowed values: ['text']).
func prepareMessages(model string, msgs []llm.Message) []llm.Message {
	if len(msgs) == 0 {
		return msgs
	}
	vision := supportsVision(model)
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m
		if len(m.Parts) == 0 {
			continue
		}
		if vision {
			cleaned := make([]llm.ContentPart, 0, len(m.Parts))
			for _, p := range m.Parts {
				if p.Type == "text" || p.Type == "image_url" {
					cleaned = append(cleaned, p)
				}
			}
			if len(cleaned) == 1 && cleaned[0].Type == "text" {
				out[i].Content = cleaned[0].Text
				out[i].Parts = nil
			} else {
				out[i].Parts = cleaned
			}
			continue
		}
		var b strings.Builder
		if strings.TrimSpace(m.Content) != "" {
			b.WriteString(m.Content)
		}
		for _, p := range m.Parts {
			switch p.Type {
			case "text":
				if p.Text == "" {
					continue
				}
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(p.Text)
			case "image_url":
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString("[изображение опущено: текущая модель Z.ai только текстовая]")
			}
		}
		out[i].Content = b.String()
		out[i].Parts = nil
	}
	return out
}

type apiErrorBody struct {
	Error struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// mapAPIError turns Z.ai JSON error payloads into short user-facing messages.
func mapAPIError(status int, body []byte) error {
	var payload apiErrorBody
	_ = json.Unmarshal(body, &payload)
	code := fmt.Sprint(payload.Error.Code)
	switch code {
	case "1305":
		return fmt.Errorf("Модель перегружена, попробуйте позднее…")
	case "1302":
		return fmt.Errorf("Превышен лимит запросов, попробуйте позднее…")
	case "1113":
		return fmt.Errorf("Недостаточно баланса Z.ai. Выберите бесплатную модель (glm-4.7-flash) или пополните счёт / включите Coding Plan")
	case "1210":
		low := strings.ToLower(payload.Error.Message)
		if strings.Contains(low, "content.type") || strings.Contains(low, "image") {
			return fmt.Errorf("Эта модель Z.ai принимает только текст (без изображений). Выберите vision-модель (например glm-5.3-flash) или уберите картинки из чата")
		}
		if strings.Contains(low, "thinking") || strings.Contains(low, "disabled") {
			return fmt.Errorf("Эта модель всегда думает — нельзя отключить thinking; используйте effort low/high/max")
		}
		if msg := strings.TrimSpace(payload.Error.Message); msg != "" {
			return fmt.Errorf("zai: %s", msg)
		}
		return fmt.Errorf("zai: неверный параметр запроса (1210)")
	}
	msg := strings.TrimSpace(payload.Error.Message)
	if msg != "" {
		return fmt.Errorf("zai: %s", msg)
	}
	return fmt.Errorf("zai: HTTP %d: %s", status, truncate(string(body), 800))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
