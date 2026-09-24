package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPStatusError — не-2xx ответ провайдера вместе с телом и заголовками:
// каждый провайдер переводит его в своё человеческое сообщение (mapAPIError).
type HTTPStatusError struct {
	Status int
	Body   []byte
	Header http.Header
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, TruncateRunes(string(e.Body), 300))
}

// StreamUnsupported сообщает, что эндпоинт отказался от SSE (например, не понял
// stream_options): провайдер может молча откатиться на обычный запрос.
func StreamUnsupported(status int) bool {
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed,
		http.StatusUnprocessableEntity, http.StatusNotImplemented:
		return true
	}
	return false
}

// StreamClient готовит HTTP-клиент под поток: без общего Timeout (иначе длинный
// ответ обрывается), с общим потоковым транспортом (протокол — из Settings →
// Network) и лимитом на заголовки ответа.
func StreamClient(base *http.Client) *http.Client {
	client := http.Client{Transport: StreamTransport()}
	if base != nil {
		client = *base
		client.Timeout = 0
		client.Transport = StreamTransport()
	}
	return &client
}

// StreamOpenAI выполняет уже подготовленный POST и разбирает SSE-поток
// Chat Completions, отдавая дельты в onDelta. Заголовки и тело запроса готовит
// провайдер — форма потока у DeepSeek, Z.ai, Qwen, OpenRouter и Local одинакова
// (см. docs/exchange-protocols/*.md).
func StreamOpenAI(ctx context.Context, client *http.Client, req *http.Request, onDelta func(StreamDelta)) (*ChatResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("stream: пустой запрос")
	}
	req.Header.Set("Accept", "text/event-stream")
	client = StreamClient(client)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		return nil, &HTTPStatusError{Status: res.StatusCode, Body: data, Header: res.Header}
	}
	return ConsumeOpenAISSE(res.Body, onDelta)
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
	Usage *Usage `json:"usage,omitempty"`
}

type toolCallAcc struct {
	id        string
	typ       string
	name      string
	arguments strings.Builder
}

// StreamIdleTimeout — сколько тишины в потоке терпим, прежде чем решить, что
// провайдер молчит. Живое, но «замолчавшее» соединение (TCP не разорван, данных
// нет) иначе висит до TCP-keepalive: именно так шаг «застывал» на 12 минут.
const StreamIdleTimeout = 120 * time.Second

// ConsumeOpenAISSE собирает финальное сообщение из SSE-делт Chat Completions:
// текст, рассуждения (reasoning_content или reasoning) и tool_calls по индексам.
func ConsumeOpenAISSE(r io.Reader, onDelta func(StreamDelta)) (*ChatResponse, error) {
	return consumeOpenAISSE(r, onDelta, StreamIdleTimeout)
}

// consumeOpenAISSE — тот же разбор с настраиваемым idle-лимитом (для тестов).
func consumeOpenAISSE(r io.Reader, onDelta func(StreamDelta), idle time.Duration) (*ChatResponse, error) {
	// Чтение идёт в отдельной горутине: так основной цикл может заметить тишину
	// и вернуть ошибку, вместо того чтобы бесконечно ждать Scan().
	lines := make(chan string, 64)
	errs := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(r)
		// Аргументы tool_calls приходят крупными кусками — поднимаем лимит токена.
		buf := make([]byte, 0, 64*1024)
		sc.Buffer(buf, 4*1024*1024)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-done:
				return
			}
		}
		errs <- sc.Err()
	}()

	var (
		id, model, finish string
		content           strings.Builder
		reasoning         strings.Builder
		tools             = map[int]*toolCallAcc{}
		usage             *Usage
	)

	idleTimer := time.NewTimer(idle)
	defer idleTimer.Stop()

	for {
		var line string
		select {
		case l, ok := <-lines:
			if !ok {
				if err := <-errs; err != nil {
					return nil, fmt.Errorf("stream read: %w", err)
				}
				return finishOpenAIStream(id, model, finish, &content, &reasoning, tools, usage), nil
			}
			line = l
			if idle > 0 {
				idleTimer.Reset(idle)
			}
		case <-idleTimer.C:
			return nil, fmt.Errorf("stream idle: провайдер молчит %s (нет данных в потоке)", humanShortDuration(idle))
		}

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
				onDelta(StreamDelta{Content: d.Content})
			}
		}
		// DeepSeek и Z.ai отдают рассуждения как reasoning_content, Ollama в
		// OpenAI-совместимом режиме и часть роутеров — как reasoning.
		think := d.ReasoningContent
		if think == "" {
			think = d.Reasoning
		}
		if think != "" {
			reasoning.WriteString(think)
			if onDelta != nil {
				onDelta(StreamDelta{ReasoningContent: think})
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
}

// finishOpenAIStream собирает итоговое сообщение из накопленных дельт.
func finishOpenAIStream(
	id, model, finish string,
	content, reasoning *strings.Builder,
	tools map[int]*toolCallAcc,
	usage *Usage,
) *ChatResponse {
	msg := Message{
		Role:             "assistant",
		Content:          content.String(),
		ReasoningContent: reasoning.String(),
	}
	if len(tools) > 0 {
		// Порядок вызовов сохраняем как 0..n.
		maxIdx := -1
		for i := range tools {
			if i > maxIdx {
				maxIdx = i
			}
		}
		calls := make([]ToolCall, 0, len(tools))
		for i := 0; i <= maxIdx; i++ {
			acc, ok := tools[i]
			if !ok {
				continue
			}
			typ := acc.typ
			if typ == "" {
				typ = "function"
			}
			callID := acc.id
			if callID == "" {
				callID = fmt.Sprintf("call_stream_%d", i+1)
			}
			args := acc.arguments.String()
			if args == "" {
				args = "{}"
			}
			calls = append(calls, ToolCall{
				ID:   callID,
				Type: typ,
				Function: FunctionCall{
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

	return &ChatResponse{
		ID:    id,
		Model: model,
		Choices: []Choice{{
			Index:        0,
			FinishReason: finish,
			Message:      msg,
		}},
		Usage: usage,
	}
}

// humanShortDuration — «2 мин», «90 с» — для сообщения о тишине в потоке.
func humanShortDuration(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	}
	return fmt.Sprintf("%d с", int(d.Seconds()))
}
