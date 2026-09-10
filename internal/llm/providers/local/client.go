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
}

func New(baseURL, apiKey, model string) *Client {
	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		baseURL: NormalizeBaseURL(baseURL),
		http: &http.Client{
			Timeout: 300 * time.Second,
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
	Model       string         `json:"model"`
	Messages    []llm.Message  `json:"messages"`
	Tools       []llm.ToolSpec `json:"tools,omitempty"`
	ToolChoice  any            `json:"tool_choice,omitempty"`
	Stream      bool           `json:"stream"`
	Temperature *float64       `json:"temperature,omitempty"`
	MaxTokens   *int           `json:"max_tokens,omitempty"`
}

type modelsListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type ollamaTagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
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
	if c.baseURL == "" {
		return nil, fmt.Errorf("local: base URL is empty")
	}
	ids, err := c.listOpenAIModels(ctx)
	if err == nil && len(ids) > 0 {
		return uniqueSorted(ids), nil
	}
	tags, tagErr := c.listOllamaTags(ctx)
	if tagErr == nil && len(tags) > 0 {
		return uniqueSorted(tags), nil
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
		return nil, mapAPIError(res.StatusCode, data)
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
		return nil, mapAPIError(res.StatusCode, data)
	}
	var out ollamaTagsResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("local: decode ollama tags: %w", err)
	}
	ids := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		if name := strings.TrimSpace(m.Name); name != "" {
			ids = append(ids, name)
		}
	}
	return ids, nil
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

	// Do not forward thinking / reasoning_effort — most local servers reject them.
	payload := apiRequest{
		Model:      model,
		Messages:   req.Messages,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		Stream:     false,
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

func mapAPIError(status int, body []byte) error {
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
		return fmt.Errorf("Local API: слишком много запросов, попробуйте позднее…")
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
