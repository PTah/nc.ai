package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"notcursor.ai/app/internal/llm"
)

const (
	DefaultBaseURL = "https://api.deepseek.com"
	DefaultModel   = "deepseek-flash"     // V4.1 Flash (native multimodal)
	VisionModel    = "deepseek-flash"     // V4.1 Flash handles images natively
	LegacyModel    = "deepseek-v4-flash"  // retired id, still routed to V4.1 Flash
	LegacyVision   = "deepseek-v4-flash-vision-exp"
	LegacyProModel = "deepseek-v4-pro"
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
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
	Temperature     *float64       `json:"temperature,omitempty"`
	MaxTokens       *int           `json:"max_tokens,omitempty"`
	Thinking        map[string]any `json:"thinking,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
}

func (c *Client) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return c.chat(ctx, req, false, nil)
}

// ChatCompletionStream — то же самое, но ответ идёт по SSE
// (docs/exchange-protocols/deepseek.md §7): дельты уходят в onDelta,
// возвращается собранное сообщение с tool_calls.
func (c *Client) ChatCompletionStream(ctx context.Context, req *llm.ChatRequest, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	return c.chat(ctx, req, true, onDelta)
}

func (c *Client) chat(ctx context.Context, req *llm.ChatRequest, stream bool, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
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
		Stream:          stream,
		StreamOptions:   includeUsage(stream),
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

	if stream {
		out, err := llm.StreamOpenAI(ctx, c.http, httpReq, onDelta)
		if err != nil {
			var status *llm.HTTPStatusError
			if errors.As(err, &status) {
				// Эндпоинт не понял stream_options — отдаём обычный ответ.
				if llm.StreamUnsupported(status.Status) {
					return c.ChatCompletion(ctx, req)
				}
				return nil, mapAPIError(status.Status, status.Body)
			}
			return nil, err
		}
		if out.Model == "" {
			out.Model = model
		}
		return out, nil
	}

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

// includeUsage просит usage в последнем SSE-чанке (DeepSeek умеет это с
// stream_options.include_usage); для обычного запроса опция не нужна.
func includeUsage(stream bool) *streamOptions {
	if !stream {
		return nil
	}
	return &streamOptions{IncludeUsage: true}
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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

// FallbackModels is used only when GET /models is unavailable (no key / network).
// V4.1 Flash is the only current DeepSeek model; legacy ids keep working as aliases.
func FallbackModels() []string {
	return []string{DefaultModel}
}

// MergeKnownModels returns the API catalog as the single source of truth:
// ids are trimmed/deduped, and fallback is used only for an empty catalog.
// Retired ids (deepseek-v4-flash, -vision-exp) are deliberately NOT injected,
// so the UI shows what DeepSeek actually serves.
func MergeKnownModels(available []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(available)+1)
	for _, id := range available {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return FallbackModels()
	}
	return out
}

// ModelDiff reports ids added or removed between two catalog snapshots.
// Used to notify the chat when DeepSeek adds/retires models.
func ModelDiff(prev, cur []string) (added, removed []string) {
	p := map[string]bool{}
	c := map[string]bool{}
	for _, id := range prev {
		p[strings.TrimSpace(id)] = true
	}
	for _, id := range cur {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		c[id] = true
		if !p[id] {
			added = append(added, id)
		}
	}
	for _, id := range prev {
		id = strings.TrimSpace(id)
		if id != "" && !c[id] {
			removed = append(removed, id)
		}
	}
	return added, removed
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

