package openrouter

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
	// DefaultBaseURL is the official OpenAI-compatible OpenRouter API root.
	// Docs: https://openrouter.ai/docs/api_reference/overview
	DefaultBaseURL = "https://openrouter.ai/api/v1"
	// DefaultModel is a cheap coding model with tool calling (:floor → lowest price).
	DefaultModel = "qwen/qwen3-coder-flash:floor"
	// StrongModel for Auto-models complex / long agent runs.
	StrongModel = "qwen/qwen3-coder:floor"
	// VisionModel for multimodal turns (tools + vision).
	VisionModel = "qwen/qwen3-vl-8b-instruct"

	AppReferer = "https://git.papatramp.ru/PapaTramp/nc.ai"
	AppTitle   = "NotCursor.ai"
)

// CuratedModels are coding / agent-oriented defaults shown when /models is unavailable.
// Prefer models that advertise tools support; :floor picks the cheapest live provider.
var CuratedModels = []string{
	"qwen/qwen3-coder-flash",
	"qwen/qwen3-coder-flash:floor",
	"qwen/qwen3-coder-30b-a3b-instruct",
	"qwen/qwen3-coder-30b-a3b-instruct:floor",
	"qwen/qwen3-coder",
	"qwen/qwen3-coder:floor",
	"qwen/qwen3-coder-plus",
	"qwen/qwen3-coder-plus:floor",
	"qwen/qwen3-coder-next",
	"qwen/qwen3-coder-next:floor",
	"qwen/qwen3-vl-8b-instruct",
	"qwen/qwen3-vl-32b-instruct",
}

// Client talks to OpenRouter OpenAI-compatible Chat Completions API.
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

func (c *Client) Name() string { return "openrouter" }

func (c *Client) SetAPIKey(key string) { c.apiKey = key }
func (c *Client) SetModel(model string) {
	if model == "" {
		model = DefaultModel
	}
	c.model = model
}

// ModelInfo is one entry from GET /models.
type ModelInfo struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name,omitempty"`
	SupportedParams    []string `json:"supported_parameters,omitempty"`
	Architecture       *struct {
		Modality string `json:"modality,omitempty"`
	} `json:"architecture,omitempty"`
}

type modelsListResponse struct {
	Data []ModelInfo `json:"data"`
}

type apiRequest struct {
	Model       string         `json:"model"`
	Messages    []llm.Message  `json:"messages"`
	Tools       []llm.ToolSpec `json:"tools,omitempty"`
	ToolChoice  any            `json:"tool_choice,omitempty"`
	Stream      bool           `json:"stream"`
	Temperature *float64       `json:"temperature,omitempty"`
	MaxTokens   *int           `json:"max_tokens,omitempty"`
}

func (c *Client) setAppHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// App attribution (optional but recommended for rankings).
	req.Header.Set("HTTP-Referer", AppReferer)
	req.Header.Set("X-OpenRouter-Title", AppTitle)
	req.Header.Set("X-Title", AppTitle) // legacy alias still accepted
}

// ListModels calls GET /models (optionally filtered to tool-capable ids).
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("openrouter: api key is empty")
	}
	url := c.baseURL + "/models?supported_parameters=tools"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.setAppHeaders(httpReq)
	httpReq.Header.Del("Content-Type")

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
		return nil, fmt.Errorf("openrouter: decode models: %w", err)
	}
	return out.Data, nil
}

// FallbackModels returns curated coding models when the catalog is unavailable.
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
	base := strings.TrimSuffix(DefaultModel, ":floor")
	if seen[base] {
		return base
	}
	return available[0]
}

// MergeCurated ensures curated ids are present even if GET /models omitted variants.
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

// OrderModels puts curated coding ids first, then the rest (stable).
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

// FilterCodingModels keeps curated + qwen/deepseek/coder-ish tool models (cap size).
func FilterCodingModels(items []ModelInfo, maxExtra int) []string {
	if maxExtra <= 0 {
		maxExtra = 40
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
		if !(strings.Contains(low, "coder") ||
			strings.Contains(low, "qwen") ||
			strings.HasPrefix(low, "deepseek/") ||
			strings.Contains(low, "devstral") ||
			strings.Contains(low, "codestral")) {
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

func (c *Client) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("openrouter: api key is empty")
	}
	if req == nil {
		return nil, fmt.Errorf("openrouter: nil request")
	}
	model := req.Model
	if model == "" {
		model = c.model
	}
	hasTools := len(req.Tools) > 0
	if hasTools {
		if err := llm.ValidateToolHistory("openrouter", req.Messages); err != nil {
			return nil, err
		}
	}

	payload := apiRequest{
		Model:      model,
		Messages:   req.Messages,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		Stream:     false, // stage-1: non-stream only (SSE later)
		MaxTokens:  req.MaxTokens,
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
	c.setAppHeaders(httpReq)

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
		return nil, fmt.Errorf("openrouter: decode: %w", err)
	}
	if out.Model == "" {
		out.Model = model
	}
	return &out, nil
}

type apiErrorBody struct {
	Error struct {
		Code     any    `json:"code"`
		Message  string `json:"message"`
		Metadata any    `json:"metadata"`
	} `json:"error"`
}

func mapAPIError(status int, body []byte) error {
	var payload apiErrorBody
	_ = json.Unmarshal(body, &payload)
	msg := strings.TrimSpace(payload.Error.Message)
	low := strings.ToLower(msg)
	switch status {
	case 402:
		return fmt.Errorf("Недостаточно кредитов OpenRouter. Пополните баланс на openrouter.ai/credits и нажмите Reconnect.")
	case 401:
		return fmt.Errorf("OpenRouter: неверный API-ключ.")
	case 403:
		if strings.Contains(low, "credit") || strings.Contains(low, "balance") {
			return fmt.Errorf("Недостаточно кредитов OpenRouter. Пополните баланс на openrouter.ai/credits.")
		}
		return fmt.Errorf("OpenRouter: доступ запрещён (403).")
	case 429:
		return fmt.Errorf("Превышен лимит запросов OpenRouter, попробуйте позднее…")
	case 500, 502, 503:
		return fmt.Errorf("OpenRouter временно недоступен (%d), попробуйте позднее…", status)
	}
	detail := msg
	if detail == "" {
		detail = llm.TruncateRunes(string(body), 400)
	}
	return fmt.Errorf("openrouter: HTTP %d: %s", status, detail)
}
