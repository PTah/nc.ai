// Package qwen talks to Alibaba Cloud Model Studio (DashScope) OpenAI-compatible
// Chat Completions API. The wire format matches OpenAI (messages, tools,
// tool_calls), so the shared agent loop works unchanged.
//
// Docs: https://www.alibabacloud.com/help/en/model-studio/compatibility-of-openai-with-dashscope
package qwen

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
	// IntlBaseURL is the international (non-mainland) compatible-mode endpoint.
	IntlBaseURL = "https://dashscope-intl.aliyuncs.com/compatible-mode/v1"
	// CnBaseURL is the mainland-China compatible-mode endpoint.
	CnBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	// DefaultBaseURL prefers the international endpoint (most users outside CN).
	DefaultBaseURL = IntlBaseURL

	// DefaultModel is a balanced price/quality model — good default for an agent.
	DefaultModel = "qwen-plus"
	// FastModel is the cheapest/fastest tier (simple tasks, classification).
	FastModel = "qwen-turbo"
	// StrongModel is the most capable text model (planning, hard reasoning).
	StrongModel = "qwen-max"
	// CoderModel is the coding-oriented flagship.
	CoderModel = "qwen3-coder-plus"
	// VisionModel is multimodal (images + text).
	VisionModel = "qwen-vl-max"

	// EndpointIntl / EndpointCn select the stored DashScope region.
	EndpointIntl = "intl"
	EndpointCn   = "cn"
)

// CuratedModels are shown when GET /models is unavailable.
var CuratedModels = []string{
	"qwen-plus",
	"qwen-turbo",
	"qwen-max",
	"qwen3-coder-plus",
	"qwen3-coder-flash",
	"qwen-vl-max",
	"qwen-vl-plus",
}

// BaseURLFor returns the compatible-mode root for a stored endpoint id.
func BaseURLFor(endpoint string) string {
	switch strings.TrimSpace(strings.ToLower(endpoint)) {
	case EndpointCn:
		return CnBaseURL
	default:
		return IntlBaseURL
	}
}

// Client talks to DashScope's OpenAI-compatible Chat Completions API.
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
		apiKey:  key(apiKey),
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 180 * time.Second,
		},
	}
}

// key trims a pasted API key; DashScope keys are sent verbatim as Bearer tokens.
func key(k string) string { return strings.TrimSpace(k) }

func (c *Client) Name() string { return "qwen" }

func (c *Client) SetAPIKey(k string) { c.apiKey = key(k) }

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

// ModelInfo is one entry from GET /models (OpenAI-compatible).
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type modelsListResponse struct {
	Object string      `json:"object"`
	Data   []ModelInfo `json:"data"`
}

// ListModels calls GET /models and returns the ids available to the key.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("qwen: api key is empty")
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
		return nil, fmt.Errorf("qwen: decode models: %w", err)
	}
	return out.Data, nil
}

// FallbackModels returns curated models when /models is unavailable.
func FallbackModels() []string {
	return append([]string{}, CuratedModels...)
}

// PreferModel keeps current if listed, else DefaultModel, else first available.
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
	if seen[DefaultModel] {
		return DefaultModel
	}
	return available[0]
}

// MergeCurated ensures curated ids are present even if GET /models omitted them.
func MergeCurated(available []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(available)+len(CuratedModels))
	for _, id := range available {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, id := range CuratedModels {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// OrderModels puts curated ids first, then the rest (stable).
func OrderModels(available []string) []string {
	if len(available) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(available))
	for _, want := range CuratedModels {
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

// FilterModels keeps curated ids + qwen-ish chat models (cap size).
func FilterModels(items []ModelInfo, maxExtra int) []string {
	if maxExtra <= 0 {
		maxExtra = 60
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(CuratedModels)+maxExtra)
	for _, id := range CuratedModels {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	extra := 0
	for _, m := range items {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		low := strings.ToLower(id)
		if !strings.Contains(low, "qwen") {
			continue
		}
		// Skip non-chat families returned by DashScope.
		if strings.Contains(low, "embedding") || strings.Contains(low, "rerank") ||
			strings.Contains(low, "tts") || strings.Contains(low, "asr") ||
			strings.Contains(low, "audio") || strings.Contains(low, "image") {
			continue
		}
		seen[id] = true
		out = append(out, id)
		extra++
		if extra >= maxExtra {
			break
		}
	}
	return out
}

type apiRequest struct {
	Model      string         `json:"model"`
	Messages   []llm.Message  `json:"messages"`
	Tools      []llm.ToolSpec `json:"tools,omitempty"`
	ToolChoice any            `json:"tool_choice,omitempty"`
	Stream     bool           `json:"stream"`
	// include_usage просим только в потоке: DashScope отдаёт usage последним чанком.
	StreamOptions *qwenStreamOptionsT `json:"stream_options,omitempty"`
	Temperature   *float64            `json:"temperature,omitempty"`
	MaxTokens     *int                `json:"max_tokens,omitempty"`
}

func (c *Client) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return c.chat(ctx, req, false, nil)
}

// ChatCompletionStream — SSE-вариант: дельты уходят в onDelta, возвращается
// собранное сообщение с tool_calls.
func (c *Client) ChatCompletionStream(ctx context.Context, req *llm.ChatRequest, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	return c.chat(ctx, req, true, onDelta)
}

func (c *Client) chat(ctx context.Context, req *llm.ChatRequest, stream bool, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("qwen: api key is empty")
	}
	if req == nil {
		return nil, fmt.Errorf("qwen: nil request")
	}
	model := req.Model
	if model == "" {
		model = c.model
	}
	if len(req.Tools) > 0 {
		if err := llm.ValidateToolHistory("qwen", req.Messages); err != nil {
			return nil, err
		}
	}

	payload := apiRequest{
		Model:         model,
		Messages:      req.Messages,
		Tools:         req.Tools,
		ToolChoice:    req.ToolChoice,
		Stream:        stream,
		StreamOptions: qwenStreamOptions(stream),
		MaxTokens:     req.MaxTokens,
	}
	if req.Temperature != nil {
		payload.Temperature = req.Temperature
	} else {
		t := 0.2
		payload.Temperature = &t
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
		return nil, fmt.Errorf("qwen: decode: %w", err)
	}
	if out.Model == "" {
		out.Model = model
	}
	return &out, nil
}

// qwenStreamOptions просит usage последним чанком (DashScope compatible-mode).
func qwenStreamOptions(stream bool) *qwenStreamOptionsT {
	if !stream {
		return nil
	}
	return &qwenStreamOptionsT{IncludeUsage: true}
}

type qwenStreamOptionsT struct {
	IncludeUsage bool `json:"include_usage"`
}

type apiErrorBody struct {
	Error struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func mapAPIError(status int, body []byte) error {
	var payload apiErrorBody
	_ = json.Unmarshal(body, &payload)
	msg := strings.TrimSpace(payload.Error.Message)
	low := strings.ToLower(msg)
	switch status {
	case 401:
		return fmt.Errorf("Qwen (DashScope): неверный API-ключ.")
	case 402, 403:
		if strings.Contains(low, "balance") || strings.Contains(low, "quota") || strings.Contains(low, "arrearage") {
			return fmt.Errorf("Qwen (DashScope): недостаточно средств или квота исчерпана. Пополните баланс в Model Studio.")
		}
		return fmt.Errorf("Qwen (DashScope): доступ запрещён (%d).", status)
	case 429:
		return fmt.Errorf("Превышен лимит запросов Qwen (DashScope), попробуйте позднее…")
	case 500, 502, 503:
		return fmt.Errorf("Qwen (DashScope) временно недоступен (%d), попробуйте позднее…", status)
	}
	detail := msg
	if detail == "" {
		detail = llm.TruncateRunes(string(body), 400)
	}
	return fmt.Errorf("qwen: HTTP %d: %s", status, detail)
}
