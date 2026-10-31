// Package kimi talks to Moonshot AI / Kimi Open Platform OpenAI-compatible
// Chat Completions API (global endpoint api.moonshot.ai).
//
// Docs: https://platform.kimi.ai/docs/api/overview
package kimi

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
	// DefaultBaseURL is the global (non-CN) OpenAI-compatible root.
	DefaultBaseURL = "https://api.moonshot.ai/v1"
	// CnBaseURL is the mainland-China OpenAI-compatible root (keys are region-isolated).
	CnBaseURL = "https://api.moonshot.cn/v1"

	// DefaultModel is the current flagship (1M context, always-on thinking).
	DefaultModel = "kimi-k3.1-pro"
	// FastModel is the high-speed variant of K3.1 Pro (~2–3× throughput).
	FastModel = "kimi-k3.1-pro-highspeed"
	// StrongModel aliases the flagship for Auto-models escalate paths.
	StrongModel = DefaultModel
	// VisionModel: K3.1 Pro accepts images/video natively.
	VisionModel = DefaultModel
	// CodeModel is the dedicated coding model (256k; K2 series, existing accounts).
	CodeModel = "kimi-k2.7-code"
)

// CuratedModels are shown when GET /models is unavailable.
var CuratedModels = []string{
	DefaultModel,
	FastModel,
	"kimi-k3",
	CodeModel,
	"kimi-k2.7-code-highspeed",
	"kimi-k2.6",
}

// Client talks to Kimi OpenAI-compatible Chat Completions API.
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
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout:   180 * time.Second,
			Transport: llm.Transport(),
		},
	}
}

func (c *Client) Name() string { return "kimi" }

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
	c.baseURL = strings.TrimRight(baseURL, "/")
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

// ChatCompletionStream — SSE-вариант; дельты (включая reasoning_content) уходят в onDelta.
func (c *Client) ChatCompletionStream(ctx context.Context, req *llm.ChatRequest, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	return c.chat(ctx, req, true, onDelta)
}

func (c *Client) chat(ctx context.Context, req *llm.ChatRequest, stream bool, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("kimi: api key is empty")
	}
	if req == nil {
		return nil, fmt.Errorf("kimi: nil request")
	}
	model := req.Model
	if model == "" {
		model = c.model
	}

	hasTools := len(req.Tools) > 0
	if hasTools {
		if err := llm.ValidateToolHistory("kimi", req.Messages); err != nil {
			return nil, err
		}
	}

	payload := apiRequest{
		Model:         model,
		Messages:      req.Messages,
		Tools:         req.Tools,
		ToolChoice:    req.ToolChoice,
		Stream:        stream,
		StreamOptions: includeUsage(stream),
		MaxTokens:     req.MaxTokens,
	}

	// K3 / K3.1: thinking always on — send reasoning_effort only (not thinking{}).
	// K2.7-code: always thinks; omit thinking.
	// K2.6: thinking.type enabled|disabled.
	switch {
	case isK3Family(model):
		effort := req.ReasoningEffort
		if effort == "" {
			effort = "high"
		}
		payload.ReasoningEffort = effort
		// temperature/top_p fixed on K3.1 Pro — do not send.
	case isK27Code(model):
		// always-on thinking; no thinking / reasoning_effort fields
	default:
		thinking := req.Thinking
		if thinking == nil {
			thinking = map[string]any{"type": "enabled"}
		}
		if hasTools {
			thinking = map[string]any{"type": "enabled"}
		}
		payload.Thinking = thinking
		if !thinkingEnabled(thinking) {
			payload.Temperature = req.Temperature
		}
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
		return nil, fmt.Errorf("kimi: decode: %w", err)
	}
	if out.Model == "" {
		out.Model = model
	}
	return &out, nil
}

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
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    any    `json:"code"`
	} `json:"error"`
}

func mapAPIError(status int, body []byte) error {
	var payload apiErrorBody
	_ = json.Unmarshal(body, &payload)
	msg := strings.TrimSpace(payload.Error.Message)
	low := strings.ToLower(msg + " " + payload.Error.Type)
	switch status {
	case 401:
		return fmt.Errorf("Kimi: неверный API-ключ (или ключ от другого региона).")
	case 402:
		return fmt.Errorf("Недостаточно баланса Kimi. Пополните счёт на platform.kimi.ai и нажмите Reconnect.")
	case 403:
		if strings.Contains(low, "balance") || strings.Contains(low, "quota") {
			return fmt.Errorf("Недостаточно баланса Kimi. Пополните счёт на platform.kimi.ai и нажмите Reconnect.")
		}
		return fmt.Errorf("Kimi: доступ запрещён (403).")
	case 404:
		if strings.Contains(low, "model") {
			return fmt.Errorf("Kimi: модель не найдена или недоступна для этого аккаунта.")
		}
	case 429:
		return fmt.Errorf("Превышен лимит запросов Kimi, попробуйте позднее…")
	case 500, 502, 503:
		return fmt.Errorf("Kimi временно недоступен (%d), попробуйте позднее…", status)
	}
	detail := msg
	if detail == "" {
		detail = llm.TruncateRunes(string(body), 400)
	}
	return fmt.Errorf("kimi: HTTP %d: %s", status, detail)
}

func thinkingEnabled(thinking map[string]any) bool {
	if thinking == nil {
		return true
	}
	t, _ := thinking["type"].(string)
	return t != "disabled"
}

func isK3Family(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "kimi-k3")
}

func isK27Code(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "kimi-k2.7-code")
}

// ModelInfo is one entry from GET /models (extra capability flags are ignored).
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type modelsListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ListModels calls GET /models.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("kimi: api key is empty")
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
		return nil, fmt.Errorf("kimi: decode models: %w", err)
	}
	return out.Data, nil
}

// FallbackModels is used when GET /models is unavailable.
func FallbackModels() []string {
	return append([]string{}, CuratedModels...)
}

// DisplayName maps an API model id to a short UI label.
func DisplayName(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case DefaultModel:
		return "Kimi K3.1 Pro"
	case FastModel:
		return "Kimi K3.1 Pro Highspeed"
	case "kimi-k3":
		return "Kimi K3"
	case CodeModel:
		return "Kimi K2.7 Code"
	case "kimi-k2.7-code-highspeed":
		return "Kimi K2.7 Code Highspeed"
	case "kimi-k2.6":
		return "Kimi K2.6"
	}
	return strings.TrimSpace(id)
}

// FilterModels keeps ids that look like Kimi models (drop unrelated catalog noise).
func FilterModels(items []ModelInfo, limit int) []string {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, m := range items {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		low := strings.ToLower(id)
		if !strings.HasPrefix(low, "kimi-") && !strings.HasPrefix(low, "moonshot-") {
			continue
		}
		seen[id] = true
		out = append(out, id)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// MergeCurated prepends curated ids that are missing from the API list.
func MergeCurated(available []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(available)+len(CuratedModels))
	for _, id := range available {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range CuratedModels {
		if seen[id] {
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

// OrderModels puts flagship first, then highspeed / k3 / code, then the rest.
func OrderModels(available []string) []string {
	if len(available) == 0 {
		return nil
	}
	priority := []string{DefaultModel, FastModel, "kimi-k3", CodeModel, "kimi-k2.7-code-highspeed", "kimi-k2.6"}
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
