// Package yandex talks to Yandex Cloud AI Studio OpenAI-compatible
// Chat Completions API (messages, tools, tool_calls).
//
// Docs:
//   https://yandex.cloud/docs/ai-studio/concepts/openai-compatibility
//   https://yandex.cloud/docs/iam/concepts/authorization/api-key
package yandex

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
	// DefaultBaseURL is the AI Studio OpenAI-compatible root.
	DefaultBaseURL = "https://ai.api.cloud.yandex.net/v1"

	// DefaultModel is YandexGPT (balanced).
	DefaultModel = "yandexgpt"
	// FastModel is the cheaper/faster lite tier.
	FastModel = "yandexgpt-lite"
	// StrongModel is the flagship text model for complex agent work.
	StrongModel = "yandexgpt"
	// CoderModel is a strong coding-oriented model hosted in AI Studio.
	CoderModel = "qwen3-235b-a22b-fp8"
)

// CuratedModels are shown when GET /models is unavailable.
var CuratedModels = []string{
	"yandexgpt-lite",
	"yandexgpt",
	"qwen3-235b-a22b-fp8",
	"gpt-oss-120b",
}

// Client talks to Yandex AI Studio OpenAI-compatible Chat Completions.
type Client struct {
	apiKey   string
	folderID string
	model    string
	baseURL  string
	http     *http.Client
}

// New builds a client against the default AI Studio base URL.
func New(apiKey, folderID, model string) *Client {
	return NewWithBaseURL(apiKey, folderID, model, DefaultBaseURL)
}

// NewWithBaseURL builds a client with an explicit OpenAI-compatible root.
func NewWithBaseURL(apiKey, folderID, model, baseURL string) *Client {
	if model == "" {
		model = DefaultModel
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		apiKey:   key(apiKey),
		folderID: strings.TrimSpace(folderID),
		model:    model,
		baseURL:  strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout:   180 * time.Second,
			Transport: llm.Transport(),
		},
	}
}

func key(k string) string { return strings.TrimSpace(k) }

func (c *Client) Name() string { return "yandex" }

func (c *Client) SetAPIKey(k string) { c.apiKey = key(k) }

func (c *Client) SetFolderID(id string) { c.folderID = strings.TrimSpace(id) }

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

// ModelURI expands a short model id into gpt://<folder>/<model>/latest.
// Full URIs (gpt://, ds://, …) are returned unchanged.
func ModelURI(folderID, model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	low := strings.ToLower(model)
	if strings.Contains(low, "://") {
		return model
	}
	folder := strings.TrimSpace(folderID)
	if folder == "" {
		return model
	}
	model = strings.TrimSuffix(model, "/")
	if strings.HasSuffix(strings.ToLower(model), "/latest") {
		return fmt.Sprintf("gpt://%s/%s", folder, model)
	}
	return fmt.Sprintf("gpt://%s/%s/latest", folder, model)
}

// ShortModelID extracts the model name from a gpt:// URI, else returns model as-is.
func ShortModelID(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	low := strings.ToLower(model)
	if !strings.Contains(low, "://") {
		return model
	}
	// gpt://folder/name[/latest] or ds://id
	rest := model
	if i := strings.Index(model, "://"); i >= 0 {
		rest = model[i+3:]
	}
	parts := strings.Split(rest, "/")
	if len(parts) >= 2 && strings.HasPrefix(low, "gpt://") {
		name := parts[1]
		if len(parts) > 2 && strings.EqualFold(parts[len(parts)-1], "latest") {
			// gpt://folder/name/latest → name (may include nested segments)
			name = strings.Join(parts[1:len(parts)-1], "/")
		} else if len(parts) > 2 {
			name = strings.Join(parts[1:], "/")
		}
		return name
	}
	return model
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
		return nil, fmt.Errorf("yandex: api key is empty")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)

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
		return nil, fmt.Errorf("yandex: decode models: %w", err)
	}
	return out.Data, nil
}

// FallbackModels returns curated models when /models is unavailable.
func FallbackModels() []string {
	return append([]string{}, CuratedModels...)
}

// PreferModel keeps current if listed, else DefaultModel, else first available.
func PreferModel(available []string, current string) string {
	current = ShortModelID(current)
	if len(available) == 0 {
		if current != "" {
			return current
		}
		return DefaultModel
	}
	seen := map[string]bool{}
	for _, id := range available {
		seen[ShortModelID(id)] = true
		seen[id] = true
	}
	if current != "" && (seen[current] || seen[ShortModelID(current)]) {
		return current
	}
	if seen[DefaultModel] {
		return DefaultModel
	}
	return ShortModelID(available[0])
}

// MergeCurated ensures curated ids are present even if GET /models omitted them.
func MergeCurated(available []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(available)+len(CuratedModels))
	for _, id := range available {
		id = ShortModelID(id)
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
			sid := ShortModelID(id)
			if sid == want && !seen[sid] {
				seen[sid] = true
				out = append(out, sid)
			}
		}
	}
	for _, id := range available {
		sid := ShortModelID(id)
		if sid == "" || seen[sid] {
			continue
		}
		seen[sid] = true
		out = append(out, sid)
	}
	return out
}

// FilterModels keeps curated ids + chat-ish models from /models (cap size).
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
		id := ShortModelID(strings.TrimSpace(m.ID))
		if id == "" || seen[id] {
			continue
		}
		low := strings.ToLower(id)
		if strings.Contains(low, "embedding") || strings.Contains(low, "emb://") ||
			strings.Contains(low, "text-search") || strings.Contains(low, "rerank") ||
			strings.Contains(low, "tts") || strings.Contains(low, "asr") ||
			strings.Contains(low, "audio") || strings.Contains(low, "speech") ||
			strings.Contains(low, "image") || strings.Contains(low, "art") {
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
	Model         string         `json:"model"`
	Messages      []llm.Message  `json:"messages"`
	Tools         []llm.ToolSpec `json:"tools,omitempty"`
	ToolChoice    any            `json:"tool_choice,omitempty"`
	Stream        bool           `json:"stream"`
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
	Temperature   *float64       `json:"temperature,omitempty"`
	MaxTokens     *int           `json:"max_tokens,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

func (c *Client) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return c.chat(ctx, req, false, nil)
}

// ChatCompletionStream streams SSE deltas via onDelta and returns the assembled message.
func (c *Client) ChatCompletionStream(ctx context.Context, req *llm.ChatRequest, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	return c.chat(ctx, req, true, onDelta)
}

func (c *Client) chat(ctx context.Context, req *llm.ChatRequest, stream bool, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("yandex: api key is empty")
	}
	if c.folderID == "" {
		return nil, fmt.Errorf("yandex: folder id is empty")
	}
	if req == nil {
		return nil, fmt.Errorf("yandex: nil request")
	}
	model := req.Model
	if model == "" {
		model = c.model
	}
	modelURI := ModelURI(c.folderID, model)
	if len(req.Tools) > 0 {
		if err := llm.ValidateToolHistory("yandex", req.Messages); err != nil {
			return nil, err
		}
	}

	payload := apiRequest{
		Model:      modelURI,
		Messages:   req.Messages,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		Stream:     stream,
		MaxTokens:  req.MaxTokens,
	}
	if stream {
		payload.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	if req.Temperature != nil {
		payload.Temperature = req.Temperature
	} else {
		t := 0.3
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
	c.setHeaders(httpReq)

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
			out.Model = modelURI
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
		return nil, fmt.Errorf("yandex: decode: %w", err)
	}
	if out.Model == "" {
		out.Model = modelURI
	}
	return &out, nil
}

func (c *Client) setHeaders(req *http.Request) {
	// Yandex Cloud API keys use "Api-Key", not Bearer (IAM docs).
	req.Header.Set("Authorization", "Api-Key "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.folderID != "" {
		req.Header.Set("x-folder-id", c.folderID)
		// OpenAI SDK "project" maps to this header on the AI Studio gateway.
		req.Header.Set("OpenAI-Project", c.folderID)
	}
}

type apiErrorBody struct {
	Error struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
	Message string `json:"message,omitempty"`
}

func mapAPIError(status int, body []byte) error {
	var payload apiErrorBody
	_ = json.Unmarshal(body, &payload)
	msg := strings.TrimSpace(payload.Error.Message)
	if msg == "" {
		msg = strings.TrimSpace(payload.Message)
	}
	low := strings.ToLower(msg)
	switch status {
	case 401:
		return fmt.Errorf("Yandex AI Studio: неверный API-ключ.")
	case 403:
		if strings.Contains(low, "folder") || strings.Contains(low, "catalog") {
			return fmt.Errorf("Yandex AI Studio: нет доступа к каталогу (folder id). Проверьте folder id и роли сервисного аккаунта.")
		}
		return fmt.Errorf("Yandex AI Studio: доступ запрещён (проверьте scope ключа yc.ai.languageModels.execute и роль ai.languageModels.user).")
	case 404:
		return fmt.Errorf("Yandex AI Studio: модель не найдена. Проверьте URI gpt://<folder>/<model>/latest и folder id.")
	case 429:
		return fmt.Errorf("Превышен лимит запросов Yandex AI Studio, попробуйте позднее…")
	case 500, 502, 503:
		return fmt.Errorf("Yandex AI Studio временно недоступен (%d), попробуйте позднее…", status)
	}
	detail := msg
	if detail == "" {
		detail = llm.TruncateRunes(string(body), 400)
	}
	return fmt.Errorf("yandex: HTTP %d: %s", status, detail)
}
