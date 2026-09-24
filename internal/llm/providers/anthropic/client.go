package anthropic

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

// Client — провайдер Anthropic Messages API. Работает и с api.anthropic.com,
// и с роутерами, отдающими /v1/messages (Selora, Atria).
type Client struct {
	apiKey  string
	model   string
	baseURL string
	http    *http.Client
}

func New(baseURL, apiKey, model string) *Client {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		baseURL: strings.TrimRight(base, "/"),
		http: &http.Client{
			Timeout:   300 * time.Second,
			Transport: llm.Transport(),
		},
	}
}

func (c *Client) Name() string { return "Anthropic" }

func (c *Client) SetAPIKey(key string) { c.apiKey = strings.TrimSpace(key) }
func (c *Client) SetBaseURL(u string) {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	if u == "" {
		u = DefaultBaseURL
	}
	c.baseURL = u
}

func (c *Client) SetModel(model string) { c.model = strings.TrimSpace(model) }
func (c *Client) Model() string         { return c.model }
func (c *Client) BaseURL() string       { return c.baseURL }

// ChatCompletion — один нестриминговый запрос к /v1/messages.
func (c *Client) ChatCompletion(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	apiReq, err := buildRequest(req, c.model)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(c.baseURL, "/messages"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)

	res, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}
	if res.StatusCode >= 300 {
		return nil, mapAPIError(res.StatusCode, data, res.Header)
	}
	var out apiResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}
	return parseResponse(&out), nil
}

type modelsResponse struct {
	Data []struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"data"`
}

// ListModels — GET /v1/models (для выбора модели в Settings).
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(c.baseURL, "/models"), nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)
	res, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}
	if res.StatusCode >= 300 {
		return nil, mapAPIError(res.StatusCode, data, res.Header)
	}
	var out modelsResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("anthropic: decode models: %w", err)
	}
	ids := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("anthropic: список моделей пуст")
	}
	return ids, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-version", APIVersion)
	if c.apiKey != "" {
		// Официальный способ — x-api-key; роутеры часто ждут ещё и Bearer.
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

// endpoint собирает URL метода: база может быть без /v1, с /v1 или уже полным
// адресом вида https://api.anthropic.com/v1/messages.
func endpoint(base, suffix string) string {
	b := strings.TrimRight(strings.TrimSpace(base), "/")
	if b == "" {
		b = DefaultBaseURL
	}
	for _, known := range []string{"/messages", "/models"} {
		b = strings.TrimSuffix(b, known)
	}
	b = strings.TrimRight(b, "/")
	if !strings.HasSuffix(b, "/v1") {
		b += "/v1"
	}
	return b + suffix
}

func mapAPIError(status int, body []byte, h http.Header) error {
	msg := llm.APIErrorMessage(body)
	if msg == "" {
		msg = llm.TruncateRunes(string(body), 400)
	}
	switch status {
	case 400:
		return fmt.Errorf("Anthropic: запрос отвергнут (400): %s", msg)
	case 401, 403:
		return fmt.Errorf("Anthropic: неверный ключ или нет доступа (%d): %s", status, msg)
	case 404:
		return fmt.Errorf("Anthropic: endpoint или модель не найдены (404). Проверьте Base URL и Model: %s", msg)
	case 413:
		return fmt.Errorf("Anthropic: запрос слишком большой (413): %s", msg)
	case 429:
		detail := msg
		if q := llm.RateLimitQuota(h); q != "" {
			if detail == "" {
				detail = q
			} else {
				detail += " (" + q + ")"
			}
		}
		return &llm.RateLimitError{Provider: "Anthropic", Detail: detail, Wait: llm.RetryAfter(h)}
	case 500, 502, 503, 504, 529:
		return fmt.Errorf("Anthropic временно недоступен (%d): %s", status, msg)
	}
	return fmt.Errorf("anthropic: HTTP %d: %s", status, msg)
}
