// Package anthropic умеет говорить на Anthropic Messages API, оставаясь для
// агента обычным llm.Provider: канон (OpenAI-shaped) ↔ Anthropic по маппингу из
// docs/exchange-protocols/anthropic.md.
package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"notcursor.ai/app/internal/llm"
)

const (
	// DefaultBaseURL — публичный Anthropic API (Selora/Atria и прочие роутеры
	// совместимы: у них тот же /v1/messages).
	DefaultBaseURL = "https://api.anthropic.com"

	// APIVersion — значение заголовка anthropic-version.
	APIVersion = "2023-06-01"

	// defaultMaxTokens — Anthropic требует max_tokens всегда; без настройки
	// агента берём такой запас.
	defaultMaxTokens = 4096
)

// apiImageSource — блок картинки: base64 (data URL) или внешняя ссылка.
type apiImageSource struct {
	Type      string `json:"type"` // base64 | url
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

// apiBlock — content-блок запроса или ответа.
type apiBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// tool_use
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Content   string `json:"content,omitempty"`
	// thinking (только в ответах)
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// image
	Source *apiImageSource `json:"source,omitempty"`
}

// apiMessage — Anthropic-сообщение: роль + блоки.
type apiMessage struct {
	Role    string     `json:"role"`
	Content []apiBlock `json:"content"`
}

// apiTool — Anthropic-описание инструмента (input_schema вместо parameters).
type apiTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

type apiRequest struct {
	Model       string         `json:"model"`
	MaxTokens   int            `json:"max_tokens"`
	System      string         `json:"system,omitempty"`
	Messages    []apiMessage   `json:"messages"`
	Tools       []apiTool      `json:"tools,omitempty"`
	ToolChoice  map[string]any `json:"tool_choice,omitempty"`
	Temperature *float64       `json:"temperature,omitempty"`
	Stream      bool           `json:"stream,omitempty"`
}

type apiUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type apiResponse struct {
	ID         string     `json:"id"`
	Model      string     `json:"model"`
	Role       string     `json:"role"`
	Content    []apiBlock `json:"content"`
	StopReason string     `json:"stop_reason"`
	Usage      *apiUsage  `json:"usage"`
	Type       string     `json:"type"`
	// error.message разбирает llm.APIErrorMessage
	Error json.RawMessage `json:"error"`
}

// buildRequest переводит канонический запрос в Anthropic-вид.
func buildRequest(req *llm.ChatRequest, model string) (*apiRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("anthropic: пустой запрос")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = strings.TrimSpace(req.Model)
	}
	if model == "" {
		return nil, fmt.Errorf("anthropic: не задана модель (Settings → Local → Model)")
	}

	maxTokens := defaultMaxTokens
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	system := systemPrompt(req.Messages)
	out := &apiRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      system,
		Messages:    buildMessages(req.Messages),
		Tools:       buildTools(req.Tools),
		ToolChoice:  toolChoice(req.ToolChoice),
		Temperature: req.Temperature,
	}
	if len(out.Messages) == 0 {
		return nil, fmt.Errorf("anthropic: нет сообщений для отправки")
	}
	return out, nil
}

// systemPrompt склеивает system-сообщения (в Anthropic это отдельное поле).
func systemPrompt(msgs []llm.Message) string {
	var parts []string
	for _, m := range msgs {
		if !strings.EqualFold(strings.TrimSpace(m.Role), "system") {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			text = partsText(m.Parts)
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func partsText(parts []llm.ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(p.Text)
		}
	}
	return b.String()
}

// buildMessages собирает user/assistant-цепочку, склеивая подряд идущие
// одинаковые роли (Anthropic ждёт чередование, а tool_result — user-turn).
func buildMessages(msgs []llm.Message) []apiMessage {
	out := make([]apiMessage, 0, len(msgs))
	for _, m := range msgs {
		switch strings.ToLower(strings.TrimSpace(m.Role)) {
		case "system":
			continue
		case "tool":
			block := apiBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}
			if n := len(out); n > 0 && out[n-1].Role == "user" && isToolResultTurn(out[n-1]) {
				out[n-1].Content = append(out[n-1].Content, block)
				continue
			}
			out = append(out, apiMessage{Role: "user", Content: []apiBlock{block}})
		case "assistant":
			blocks := assistantBlocks(m)
			if len(blocks) == 0 {
				continue
			}
			if n := len(out); n > 0 && out[n-1].Role == "assistant" {
				out[n-1].Content = append(out[n-1].Content, blocks...)
				continue
			}
			out = append(out, apiMessage{Role: "assistant", Content: blocks})
		default:
			blocks := userBlocks(m)
			if len(blocks) == 0 {
				continue
			}
			if n := len(out); n > 0 && out[n-1].Role == "user" && !isToolResultTurn(out[n-1]) {
				out[n-1].Content = append(out[n-1].Content, blocks...)
				continue
			}
			out = append(out, apiMessage{Role: "user", Content: blocks})
		}
	}
	return out
}

func isToolResultTurn(m apiMessage) bool {
	return len(m.Content) > 0 && m.Content[0].Type == "tool_result"
}

func userBlocks(m llm.Message) []apiBlock {
	var out []apiBlock
	if len(m.Parts) > 0 {
		for _, p := range m.Parts {
			switch p.Type {
			case "text":
				if strings.TrimSpace(p.Text) != "" {
					out = append(out, apiBlock{Type: "text", Text: p.Text})
				}
			case "image_url":
				if p.ImageURL == nil || strings.TrimSpace(p.ImageURL.URL) == "" {
					continue
				}
				if src, ok := imageSource(p.ImageURL.URL); ok {
					out = append(out, apiBlock{Type: "image", Source: src})
				}
			}
		}
		if len(out) == 0 && strings.TrimSpace(m.Content) != "" {
			out = append(out, apiBlock{Type: "text", Text: m.Content})
		}
		return out
	}
	if strings.TrimSpace(m.Content) != "" {
		out = append(out, apiBlock{Type: "text", Text: m.Content})
	}
	return out
}

// imageSource превращает data URL в base64-блок, обычную ссылку — в url-блок.
func imageSource(raw string) (*apiImageSource, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	if !strings.HasPrefix(raw, "data:") {
		return &apiImageSource{Type: "url", URL: raw}, true
	}
	rest := strings.TrimPrefix(raw, "data:")
	semi := strings.Index(rest, ";")
	if semi < 0 {
		return nil, false
	}
	mediaType := rest[:semi]
	data := rest[semi+1:]
	if !strings.HasPrefix(data, "base64,") {
		return nil, false
	}
	data = strings.TrimPrefix(data, "base64,")
	if data == "" || mediaType == "" {
		return nil, false
	}
	return &apiImageSource{Type: "base64", MediaType: mediaType, Data: data}, true
}

func assistantBlocks(m llm.Message) []apiBlock {
	var out []apiBlock
	if strings.TrimSpace(m.Content) != "" {
		out = append(out, apiBlock{Type: "text", Text: m.Content})
	}
	for i, call := range m.ToolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			// Anthropic требует id у tool_use: без него не свяжем tool_result.
			id = fmt.Sprintf("toolu_%d", i+1)
		}
		out = append(out, apiBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: parseArguments(call.Function.Arguments),
		})
	}
	return out
}

// parseArguments превращает JSON-строку аргументов в объект input.
func parseArguments(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		// Модель иногда отдаёт не-JSON: отдаём как есть, чтобы не терять данные.
		return map[string]any{"raw": raw}
	}
	return out
}

func buildTools(specs []llm.ToolSpec) []apiTool {
	out := make([]apiTool, 0, len(specs))
	for _, s := range specs {
		if s.Type != "" && s.Type != "function" {
			continue
		}
		name := strings.TrimSpace(s.Function.Name)
		if name == "" {
			continue
		}
		schema := s.Function.Parameters
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, apiTool{
			Name:        name,
			Description: s.Function.Description,
			InputSchema: schema,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// toolChoice переводит канонический tool_choice в формат Anthropic.
func toolChoice(choice any) map[string]any {
	switch v := choice.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "auto", "":
			return nil
		case "required", "any":
			return map[string]any{"type": "any"}
		}
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			if name, _ := fn["name"].(string); strings.TrimSpace(name) != "" {
				return map[string]any{"type": "tool", "name": strings.TrimSpace(name)}
			}
		}
	}
	return nil
}

// parseResponse переводит ответ Anthropic в канон.
func parseResponse(resp *apiResponse) *llm.ChatResponse {
	msg := llm.Message{Role: "assistant"}
	var text, think strings.Builder
	var calls []llm.ToolCall
	for _, b := range resp.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "thinking":
			think.WriteString(b.Thinking)
		case "tool_use":
			args, err := json.Marshal(b.Input)
			if err != nil || b.Input == nil {
				args = []byte("{}")
			}
			calls = append(calls, llm.ToolCall{
				ID:       b.ID,
				Type:     "function",
				Function: llm.FunctionCall{Name: b.Name, Arguments: string(args)},
			})
		}
	}
	msg.Content = text.String()
	msg.ReasoningContent = think.String()
	msg.ToolCalls = calls

	out := &llm.ChatResponse{
		ID:    resp.ID,
		Model: resp.Model,
		Choices: []llm.Choice{{
			Message:      msg,
			FinishReason: finishReason(resp.StopReason),
		}},
	}
	if resp.Usage != nil {
		u := resp.Usage
		out.Usage = &llm.Usage{
			PromptTokens:          u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens,
			CompletionTokens:      u.OutputTokens,
			TotalTokens:           u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens + u.OutputTokens,
			PromptCacheHitTokens:  u.CacheReadInputTokens,
			PromptCacheMissTokens: u.InputTokens,
		}
	}
	return out
}

// finishReason приводит stop_reason к каноническим значениям агента.
func finishReason(stop string) string {
	switch strings.TrimSpace(stop) {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	default:
		return "stop"
	}
}
