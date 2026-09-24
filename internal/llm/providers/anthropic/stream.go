package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"notcursor.ai/app/internal/llm"
)

// ChatCompletionStream — Messages API в режиме stream=true: текстовые дельты и
// thinking уходят в onDelta, tool_use собирается из input_json_delta.
// События описаны в docs/exchange-protocols/anthropic.md.
func (c *Client) ChatCompletionStream(ctx context.Context, req *llm.ChatRequest, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	apiReq, err := buildRequest(req, c.model)
	if err != nil {
		return nil, err
	}
	apiReq.Stream = true
	body, err := json.Marshal(apiReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(c.baseURL, "/messages"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Accept", "text/event-stream")

	// Поток может идти дольше обычного таймаута: снимаем Timeout, отмена — ctx.
	client := c.http
	if client != nil && client.Timeout > 0 {
		clone := *client
		clone.Timeout = 0
		client = &clone
	}
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return nil, mapAPIError(res.StatusCode, data, res.Header)
	}
	return consumeAnthropicSSE(res.Body, onDelta)
}

type streamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

type toolAcc struct {
	id   string
	name string
	args strings.Builder
}

// consumeAnthropicSSE собирает финальный ответ из потока событий Anthropic.
func consumeAnthropicSSE(r io.Reader, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)

	var (
		id, model, stop string
		content         strings.Builder
		reasoning       strings.Builder
		tools           = map[int]*toolAcc{}
		promptTokens    int
		cacheRead       int
		cacheCreate     int
		outputTokens    int
	)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil {
				id = ev.Message.ID
				model = ev.Message.Model
				if u := ev.Message.Usage; u != nil {
					promptTokens += u.InputTokens
					cacheRead += u.CacheReadInputTokens
					cacheCreate += u.CacheCreationInputTokens
					outputTokens += u.OutputTokens
				}
			}
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				tools[ev.Index] = &toolAcc{id: ev.ContentBlock.ID, name: ev.ContentBlock.Name}
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" {
					content.WriteString(ev.Delta.Text)
					if onDelta != nil {
						onDelta(llm.StreamDelta{Content: ev.Delta.Text})
					}
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					reasoning.WriteString(ev.Delta.Thinking)
					if onDelta != nil {
						onDelta(llm.StreamDelta{ReasoningContent: ev.Delta.Thinking})
					}
				}
			case "input_json_delta":
				if acc, ok := tools[ev.Index]; ok && ev.Delta.PartialJSON != "" {
					acc.args.WriteString(ev.Delta.PartialJSON)
				}
			}
		case "message_delta":
			if ev.Delta != nil && strings.TrimSpace(ev.Delta.StopReason) != "" {
				stop = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				outputTokens += ev.Usage.OutputTokens
			}
		case "error":
			msg := "unknown error"
			if ev.Error != nil {
				msg = strings.TrimSpace(ev.Error.Message)
				if msg == "" {
					msg = ev.Error.Type
				}
			}
			return nil, fmt.Errorf("Anthropic: ошибка потока: %s", msg)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("anthropic: stream read: %w", err)
	}

	msg := llm.Message{
		Role:             "assistant",
		Content:          content.String(),
		ReasoningContent: reasoning.String(),
	}
	if len(tools) > 0 {
		maxIdx := -1
		for i := range tools {
			if i > maxIdx {
				maxIdx = i
			}
		}
		calls := make([]llm.ToolCall, 0, len(tools))
		for i := 0; i <= maxIdx; i++ {
			acc, ok := tools[i]
			if !ok {
				continue
			}
			args := strings.TrimSpace(acc.args.String())
			if args == "" {
				args = "{}"
			}
			callID := acc.id
			if callID == "" {
				callID = fmt.Sprintf("toolu_stream_%d", i+1)
			}
			calls = append(calls, llm.ToolCall{
				ID:       callID,
				Type:     "function",
				Function: llm.FunctionCall{Name: acc.name, Arguments: args},
			})
		}
		msg.ToolCalls = calls
	}

	out := &llm.ChatResponse{
		ID:    id,
		Model: model,
		Choices: []llm.Choice{{
			Message:      msg,
			FinishReason: finishReason(stop),
		}},
	}
	if promptTokens > 0 || outputTokens > 0 || cacheRead > 0 || cacheCreate > 0 {
		in := promptTokens + cacheRead + cacheCreate
		out.Usage = &llm.Usage{
			PromptTokens:          in,
			CompletionTokens:      outputTokens,
			TotalTokens:           in + outputTokens,
			PromptCacheHitTokens:  cacheRead,
			PromptCacheMissTokens: promptTokens,
		}
	}
	return out, nil
}
