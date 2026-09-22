package local

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
	hasTools := len(req.Tools) > 0
	if hasTools {
		if err := llm.ValidateToolHistory("local", req.Messages); err != nil {
			return nil, err
		}
	}

	payload := apiRequest{
		Model:      model,
		Messages:   req.Messages,
		Tools:      req.Tools,
		ToolChoice: req.ToolChoice,
		Stream:     true,
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
	httpReq.Header.Set("Accept", "text/event-stream")

	// No overall Timeout: stream may run longer than a single completion; ctx cancels.
	httpClient := c.http
	if httpClient != nil && httpClient.Timeout > 0 {
		clone := *httpClient
		clone.Timeout = 0
		httpClient = &clone
	}

	res, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return nil, mapAPIError(res.StatusCode, data)
	}

	agg, err := consumeOpenAISSE(res.Body, onDelta)
	if err != nil {
		return nil, err
	}
	if agg.Model == "" {
		agg.Model = model
	}
	return agg, nil
}

type streamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Delta        struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *llm.Usage `json:"usage,omitempty"`
}

type toolCallAcc struct {
	id        string
	typ       string
	name      string
	arguments strings.Builder
}

func consumeOpenAISSE(r io.Reader, onDelta func(llm.StreamDelta)) (*llm.ChatResponse, error) {
	sc := bufio.NewScanner(r)
	// Tool-call argument chunks can be large; raise the token limit.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)

	var (
		id, model, finish string
		content           strings.Builder
		reasoning         strings.Builder
		tools             = map[int]*toolCallAcc{}
		usage             *llm.Usage
	)

	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.ID != "" {
			id = chunk.ID
		}
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		ch := chunk.Choices[0]
		if fr := strings.TrimSpace(ch.FinishReason); fr != "" {
			finish = fr
		}
		d := ch.Delta
		if d.Content != "" {
			content.WriteString(d.Content)
			if onDelta != nil {
				onDelta(llm.StreamDelta{Content: d.Content})
			}
		}
		// DeepSeek отдаёт рассуждения как reasoning_content, Ollama в
		// OpenAI-совместимом режиме — как reasoning.
		think := d.ReasoningContent
		if think == "" {
			think = d.Reasoning
		}
		if think != "" {
			reasoning.WriteString(think)
			if onDelta != nil {
				onDelta(llm.StreamDelta{ReasoningContent: think})
			}
		}
		for _, tc := range d.ToolCalls {
			acc, ok := tools[tc.Index]
			if !ok {
				acc = &toolCallAcc{}
				tools[tc.Index] = acc
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Type != "" {
				acc.typ = tc.Type
			}
			if tc.Function.Name != "" {
				acc.name += tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.arguments.WriteString(tc.Function.Arguments)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("local: stream read: %w", err)
	}

	msg := llm.Message{
		Role:             "assistant",
		Content:          content.String(),
		ReasoningContent: reasoning.String(),
	}
	if len(tools) > 0 {
		// Preserve index order 0..n
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
			typ := acc.typ
			if typ == "" {
				typ = "function"
			}
			id := acc.id
			if id == "" {
				id = fmt.Sprintf("call_stream_%d", i+1)
			}
			args := acc.arguments.String()
			if args == "" {
				args = "{}"
			}
			calls = append(calls, llm.ToolCall{
				ID:   id,
				Type: typ,
				Function: llm.FunctionCall{
					Name:      acc.name,
					Arguments: args,
				},
			})
		}
		msg.ToolCalls = calls
	}

	if finish == "" {
		if len(msg.ToolCalls) > 0 {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}

	return &llm.ChatResponse{
		ID:    id,
		Model: model,
		Choices: []llm.Choice{{
			Index:        0,
			FinishReason: finish,
			Message:      msg,
		}},
		Usage: usage,
	}, nil
}
