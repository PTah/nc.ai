package anthropic

import (
	"encoding/json"
	"testing"

	"notcursor.ai/app/internal/llm"
)

func TestBuildRequestMapsCanonToAnthropic(t *testing.T) {
	temp := 0.3
	maxTok := 1234
	req := &llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "ты агент"},
			{Role: "user", Content: "привет"},
			{Role: "assistant", Content: "зову инструмент", ToolCalls: []llm.ToolCall{{
				ID: "toolu_1", Type: "function",
				Function: llm.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`},
			}}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "package a"},
		},
		Tools: []llm.ToolSpec{{
			Type: "function",
			Function: llm.ToolSpecFunction{
				Name:        "read_file",
				Description: "Read file",
				Parameters:  map[string]any{"type": "object"},
			},
		}},
		ToolChoice:  "required",
		Temperature: &temp,
		MaxTokens:   &maxTok,
	}
	out, err := buildRequest(req, "claude-sonnet-4")
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if out.Model != "claude-sonnet-4" || out.MaxTokens != 1234 {
		t.Errorf("model=%q maxTokens=%d", out.Model, out.MaxTokens)
	}
	if out.System != "ты агент" {
		t.Errorf("system=%q", out.System)
	}
	if out.Temperature == nil || *out.Temperature != 0.3 {
		t.Errorf("temperature=%v", out.Temperature)
	}
	if len(out.Tools) != 1 || out.Tools[0].Name != "read_file" || out.Tools[0].InputSchema["type"] != "object" {
		t.Fatalf("tools=%+v", out.Tools)
	}
	if out.ToolChoice["type"] != "any" {
		t.Errorf("tool_choice=%v", out.ToolChoice)
	}
	if len(out.Messages) != 3 {
		t.Fatalf("messages=%+v", out.Messages)
	}
	if out.Messages[0].Role != "user" || out.Messages[0].Content[0].Text != "привет" {
		t.Errorf("первый turn: %+v", out.Messages[0])
	}
	call := out.Messages[1]
	if call.Role != "assistant" || call.Content[0].Type != "text" {
		t.Errorf("assistant: %+v", call)
	}
	if use := call.Content[1]; use.Type != "tool_use" || use.Name != "read_file" || use.Input["path"] != "a.go" {
		t.Errorf("tool_use: %+v", use)
	}
	res := out.Messages[2]
	if res.Role != "user" || res.Content[0].Type != "tool_result" || res.Content[0].ToolUseID != "toolu_1" {
		t.Errorf("tool_result: %+v", res)
	}
}

func TestBuildRequestGroupsToolResultsAndKeepsRoles(t *testing.T) {
	req := &llm.ChatRequest{Messages: []llm.Message{
		{Role: "user", Content: "one"},
		{Role: "user", Content: "two"},
		{Role: "tool", ToolCallID: "t1", Content: "r1"},
		{Role: "tool", ToolCallID: "t2", Content: "r2"},
		{Role: "user", Content: "дальше"},
	}}
	out, err := buildRequest(req, "m")
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	if len(out.Messages) != 3 {
		t.Fatalf("messages=%+v", out.Messages)
	}
	if len(out.Messages[0].Content) != 2 {
		t.Errorf("подряд идущие user не склеены: %+v", out.Messages[0])
	}
	if len(out.Messages[1].Content) != 2 || out.Messages[1].Content[1].ToolUseID != "t2" {
		t.Errorf("tool_result не сгруппированы: %+v", out.Messages[1])
	}
	// tool_result и обычный текст не склеиваем: Anthropic ждёт результат первым.
	if len(out.Messages[2].Content) != 1 || out.Messages[2].Content[0].Type != "text" {
		t.Errorf("текст после tool_result: %+v", out.Messages[2])
	}
}

func TestBuildRequestRequiresModel(t *testing.T) {
	if _, err := buildRequest(&llm.ChatRequest{Messages: []llm.Message{{Role: "user", Content: "x"}}}, ""); err == nil {
		t.Fatal("ожидали ошибку про пустую модель")
	}
	if _, err := buildRequest(&llm.ChatRequest{}, "m"); err == nil {
		t.Fatal("ожидали ошибку про отсутствие сообщений")
	}
}

func TestBuildRequestImages(t *testing.T) {
	req := &llm.ChatRequest{Messages: []llm.Message{
		llm.UserMultimodal("что на картинке", []string{
			"data:image/png;base64,AAAA",
			"https://example.com/x.jpg",
		}),
	}}
	out, err := buildRequest(req, "m")
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	blocks := out.Messages[0].Content
	if len(blocks) != 3 {
		t.Fatalf("blocks=%+v", blocks)
	}
	if blocks[1].Type != "image" || blocks[1].Source.Type != "base64" || blocks[1].Source.MediaType != "image/png" {
		t.Errorf("data URL: %+v", blocks[1])
	}
	if blocks[2].Source.Type != "url" || blocks[2].Source.URL != "https://example.com/x.jpg" {
		t.Errorf("url: %+v", blocks[2])
	}
}

func TestParseResponse(t *testing.T) {
	raw := `{
	  "id": "msg_1",
	  "model": "claude-sonnet-4",
	  "content": [
	    {"type": "thinking", "thinking": "думаю", "signature": "sig"},
	    {"type": "text", "text": "готово"},
	    {"type": "tool_use", "id": "toolu_9", "name": "read_file", "input": {"path": "b.go"}}
	  ],
	  "stop_reason": "tool_use",
	  "usage": {"input_tokens": 10, "output_tokens": 5, "cache_read_input_tokens": 2}
	}`
	var resp apiResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out := parseResponse(&resp)
	msg := out.Choices[0].Message
	if msg.Content != "готово" || msg.ReasoningContent != "думаю" {
		t.Errorf("content=%q reasoning=%q", msg.Content, msg.ReasoningContent)
	}
	if out.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish=%q", out.Choices[0].FinishReason)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "toolu_9" || msg.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("tool_calls=%+v", msg.ToolCalls)
	}
	if msg.ToolCalls[0].Function.Arguments != `{"path":"b.go"}` {
		t.Errorf("arguments=%q", msg.ToolCalls[0].Function.Arguments)
	}
	if out.Usage == nil || out.Usage.PromptTokens != 12 || out.Usage.PromptCacheHitTokens != 2 {
		t.Errorf("usage=%+v", out.Usage)
	}
	if out.Model != "claude-sonnet-4" || out.ID != "msg_1" {
		t.Errorf("model=%q id=%q", out.Model, out.ID)
	}
}

func TestFinishReason(t *testing.T) {
	cases := map[string]string{
		"end_turn":      "stop",
		"stop_sequence": "stop",
		"max_tokens":    "length",
		"tool_use":      "tool_calls",
		"":              "stop",
	}
	for in, want := range cases {
		if got := finishReason(in); got != want {
			t.Errorf("finishReason(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestEndpointURL(t *testing.T) {
	cases := map[string]string{
		"https://api.anthropic.com":             "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/":            "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/v1":          "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com/v1/messages": "https://api.anthropic.com/v1/messages",
		"https://api.selora.ai/v1/":             "https://api.selora.ai/v1/messages",
		"https://api.atria-asi.ai/v1":           "https://api.atria-asi.ai/v1/messages",
		"http://127.0.0.1:8080/anthropic":       "http://127.0.0.1:8080/anthropic/v1/messages",
		"":                                      DefaultBaseURL + "/v1/messages",
	}
	for base, want := range cases {
		if got := endpoint(base, "/messages"); got != want {
			t.Errorf("endpoint(%q)=%q, want %q", base, got, want)
		}
	}
	if got := endpoint("https://api.anthropic.com/v1/messages", "/models"); got != "https://api.anthropic.com/v1/models" {
		t.Errorf("models URL=%q", got)
	}
	err := mapAPIError(503, []byte(`{"error":{"message":"Overloaded"}}`), nil)
	if err.Error() != "Anthropic временно недоступен (503): Overloaded" {
		t.Errorf("503: %q", err.Error())
	}
}
