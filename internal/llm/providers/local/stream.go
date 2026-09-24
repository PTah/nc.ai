package local

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"notcursor.ai/app/internal/llm"
)

// ChatCompletionStream calls OpenAI-compatible SSE (/v1/chat/completions stream=true)
// and aggregates the final assistant message (content + tool_calls).
func (c *Client) ChatCompletionStream(ctx context.Context, req *llm.ChatRequest, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
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
	if len(req.Tools) > 0 {
		if err := llm.ValidateToolHistory("local", req.Messages); err != nil {
			return nil, err
		}
	}

	payload := apiRequest{
		Model:           model,
		Messages:        req.Messages,
		Tools:           req.Tools,
		ToolChoice:      req.ToolChoice,
		Stream:          true,
		ReasoningEffort: strings.TrimSpace(c.reasoning),
		StreamOptions:   &streamOptions{IncludeUsage: true},
	}
	if req.Temperature != nil {
		payload.Temperature = req.Temperature
	} else {
		t := 0.2
		payload.Temperature = &t
	}
	payload.MaxTokens = req.MaxTokens

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)

	out, err := llm.StreamOpenAI(ctx, c.http, httpReq, onDelta)
	if err != nil {
		var status *llm.HTTPStatusError
		if errors.As(err, &status) {
			// Сервер не понял stream_options — отдаём обычный ответ, дельт не будет.
			if llm.StreamUnsupported(status.Status) {
				return c.ChatCompletion(ctx, req)
			}
			return nil, mapAPIError(status.Status, status.Body, status.Header)
		}
		return nil, fmt.Errorf("local: %w", err)
	}
	if out.Model == "" {
		out.Model = model
	}
	return out, nil
}

// streamOptions — OpenAI-совместимые опции потока: просим usage в последнем чанке.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// consumeOpenAISSE оставлен как тонкая обёртка над общим разбором SSE
// (llm.ConsumeOpenAISSE) — на неё опираются тесты пакета.
func consumeOpenAISSE(r io.Reader, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	return llm.ConsumeOpenAISSE(r, onDelta)
}
