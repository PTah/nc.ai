package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"notcursor.ai/app/internal/llm"
)

const (
	// DefaultBaseURL is Ollama's OpenAI-compatible API root on localhost.
	DefaultBaseURL = "http://127.0.0.1:11434/v1"
)

// Client talks to any OpenAI-compatible Chat Completions endpoint (Ollama, LM Studio, vLLM, …).
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
	// reasoning — reasoning_effort для этого эндпоинта ("" = не отправлять).
	reasoning string
	// numCtx — размер контекста из настроек сервера (0 = не задан). В запрос он
	// НЕ уходит: нестандартные поля (options.num_ctx) строгие OpenAI-серверы
	// (Lemonade, vLLM, LM Studio) могут отвергнуть с 400, а Ollama в
	// OpenAI-режиме его всё равно игнорирует (ollama#5356) — контекст там
	// задаётся на сервере. Значение нужно приложению для индикатора заполнения
	// окна и предупреждений.
	numCtx int
	// ollamaState: 0 — неизвестно, 1 — Ollama, -1 — не Ollama (LM Studio,
	// Lemonade, vLLM): у них нет /api/ps и /api/generate, не дёргаем их зря.
	ollamaState int32
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		baseURL: NormalizeBaseURL(baseURL),
		http: &http.Client{
			Timeout:   300 * time.Second,
			Transport: llm.Transport(),
		},
	}
}

func (c *Client) Name() string { return "local" }

func (c *Client) SetAPIKey(key string) { c.apiKey = strings.TrimSpace(key) }

func (c *Client) SetModel(model string) {
	c.model = strings.TrimSpace(model)
}

func (c *Client) SetBaseURL(baseURL string) {
	c.baseURL = NormalizeBaseURL(baseURL)
}

func (c *Client) BaseURL() string { return c.baseURL }
func (c *Client) Model() string   { return c.model }

// SetNumCtx задаёт размер контекста сервера (0 = не задан).
func (c *Client) SetNumCtx(n int) {
	if n < 0 {
		n = 0
	}
	c.numCtx = n
}

// NumCtx возвращает размер контекста сервера (0 = не задан).
func (c *Client) NumCtx() int { return c.numCtx }

// SetReasoningEffort включает reasoning_effort в запрос ("" = не отправлять).
func (c *Client) SetReasoningEffort(effort string) {
	c.reasoning = strings.TrimSpace(effort)
}

// isKnownNotOllama — сервер уже опознали как не-Ollama.
func (c *Client) isKnownNotOllama() bool { return atomic.LoadInt32(&c.ollamaState) < 0 }

// markOllama / markNotOllama запоминают тип сервера по ответам /api/ps и /api/generate.
func (c *Client) markOllama()    { atomic.StoreInt32(&c.ollamaState, 1) }
func (c *Client) markNotOllama() { atomic.StoreInt32(&c.ollamaState, -1) }

// ServerKind — тип сервера за OpenAI-совместимым эндпоинтом: "ollama", "openai"
// (Lemonade, LM Studio, vLLM) или "" — ещё не определили. Определяется по
// /api/ps и /api/generate: они есть только у Ollama.
func (c *Client) ServerKind() string {
	switch atomic.LoadInt32(&c.ollamaState) {
	case 1:
		return "ollama"
	case -1:
		return "openai"
	default:
		return ""
	}
}

// NormalizeBaseURL trims, adds http:// if missing, strips trailing slash.
func NormalizeBaseURL(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return DefaultBaseURL
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	return strings.TrimRight(u, "/")
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
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
}

type modelsListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type ollamaTagsResponse struct {
	Models []struct {
		Name         string   `json:"name"`
		Capabilities []string `json:"capabilities"`
		Details      struct {
			ParameterSize string `json:"parameter_size"`
		} `json:"details"`
	} `json:"models"`
}

// ModelInfo is a local server model id plus optional Ollama capabilities.
type ModelInfo struct {
	ID           string
	Capabilities []string
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// ListModels tries GET /models, then Ollama GET /api/tags as fallback.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	infos, err := c.ListModelInfos(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(infos))
	for _, m := range infos {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// ListModelInfos returns ids; when served by Ollama /api/tags, includes capabilities.
func (c *Client) ListModelInfos(ctx context.Context) ([]ModelInfo, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("local: base URL is empty")
	}
	ids, err := c.listOpenAIModels(ctx)
	if err == nil && len(ids) > 0 {
		out := make([]ModelInfo, 0, len(ids))
		for _, id := range uniqueSorted(ids) {
			out = append(out, ModelInfo{ID: id})
		}
		// Enrich with Ollama capabilities when possible (same origin).
		if tags, tagErr := c.listOllamaTagInfos(ctx); tagErr == nil && len(tags) > 0 {
			byName := map[string]ModelInfo{}
			for _, t := range tags {
				byName[t.ID] = t
			}
			for i := range out {
				if t, ok := byName[out[i].ID]; ok {
					out[i].Capabilities = t.Capabilities
				}
			}
		}
		return out, nil
	}
	tags, tagErr := c.listOllamaTagInfos(ctx)
	if tagErr == nil && len(tags) > 0 {
		return tags, nil
	}
	if err != nil {
		if tagErr != nil {
			return nil, fmt.Errorf("local: list models: %w (ollama tags: %v)", err, tagErr)
		}
		return nil, err
	}
	if tagErr != nil {
		return nil, tagErr
	}
	return nil, fmt.Errorf("local: no models returned")
}

func (c *Client) listOpenAIModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)
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
		return nil, mapAPIError(res.StatusCode, data, res.Header)
	}
	var out modelsListResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("local: decode models: %w", err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (c *Client) listOllamaTags(ctx context.Context) ([]string, error) {
	infos, err := c.listOllamaTagInfos(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(infos))
	for _, m := range infos {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func (c *Client) listOllamaTagInfos(ctx context.Context) ([]ModelInfo, error) {
	origin, err := ollamaOrigin(c.baseURL)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)
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
		return nil, mapAPIError(res.StatusCode, data, res.Header)
	}
	var out ollamaTagsResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("local: decode ollama tags: %w", err)
	}
	infos := make([]ModelInfo, 0, len(out.Models))
	seen := map[string]bool{}
	for _, m := range out.Models {
		name := strings.TrimSpace(m.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		infos = append(infos, ModelInfo{ID: name, Capabilities: append([]string{}, m.Capabilities...)})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos, nil
}

// ollamaOrigin strips a trailing /v1 path segment for Ollama native API.
func ollamaOrigin(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	path := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(path, "/v1") {
		u.Path = strings.TrimSuffix(path, "/v1")
		if u.Path == "" {
			u.Path = ""
		}
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func (c *Client) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("local: base URL is empty")
	}
	if req == nil {
		return nil, fmt.Errorf("local: nil request")
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = c.model
	}
	if model == "" {
		return nil, fmt.Errorf("local: model is empty")
	}
	hasTools := len(req.Tools) > 0
	if hasTools {
		if err := llm.ValidateToolHistory("local", req.Messages); err != nil {
			return nil, err
		}
	}

	// thinking не отправляем никогда, а reasoning_effort — только если профиль
	// эндпоинта его включил (см. c.reasoning).
	payload := apiRequest{
		Model:           model,
		Messages:        req.Messages,
		Tools:           req.Tools,
		ToolChoice:      req.ToolChoice,
		Stream:          false,
		ReasoningEffort: strings.TrimSpace(c.reasoning),
		MaxTokens:       req.MaxTokens,
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
		return nil, mapAPIError(res.StatusCode, data, res.Header)
	}
	var out llm.ChatResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("local: decode: %w", err)
	}
	if out.Model == "" {
		out.Model = model
	}
	return &out, nil
}

type apiErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
	// Ollama sometimes returns { "error": "string" }
	ErrorString string `json:"-"`
}

func mapAPIError(status int, body []byte, h http.Header) error {
	var payload apiErrorBody
	_ = json.Unmarshal(body, &payload)
	msg := strings.TrimSpace(payload.Error.Message)
	if msg == "" {
		var alt struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &alt) == nil {
			msg = strings.TrimSpace(alt.Error)
		}
	}
	switch status {
	case 401:
		return fmt.Errorf("Local API: неверный токен (401).")
	case 404:
		return fmt.Errorf("Local API: endpoint не найден (404). Проверьте Base URL (нужен суффикс /v1).")
	case 429:
		// Лимиты бесплатных тарифов (5 RPM / 200 RPD): сообщаем, когда можно
		// повторить, и сколько запросов осталось — агент подождёт сам.
		detail := msg
		if q := llm.RateLimitQuota(h); q != "" {
			if detail == "" {
				detail = q
			} else {
				detail += " (" + q + ")"
			}
		}
		return &llm.RateLimitError{Provider: "Local API", Detail: detail, Wait: llm.RetryAfter(h)}
	case 500, 502, 503:
		return fmt.Errorf("Local API временно недоступен (%d).", status)
	}
	detail := msg
	if detail == "" {
		detail = llm.TruncateRunes(string(body), 400)
	}
	return fmt.Errorf("local: HTTP %d: %s", status, detail)
}

func uniqueSorted(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
