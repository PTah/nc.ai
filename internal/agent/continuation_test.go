package agent

import (
	"testing"

	"notcursor.ai/app/internal/llm"
)

func respWith(content, reasoning string) *llm.ChatResponse {
	return &llm.ChatResponse{Choices: []llm.Choice{{Message: llm.Message{
		Role:             "assistant",
		Content:          content,
		ReasoningContent: reasoning,
	}}}}
}

func TestAssistantText(t *testing.T) {
	if assistantText(respWith("привет", "думал")) != "привет" {
		t.Fatal("должен вернуть контент ответа")
	}
	if assistantText(respWith("", "только рассуждение")) != "" {
		t.Fatal("пустой контент — пустая строка")
	}
	if assistantText(nil) != "" {
		t.Fatal("nil — пустая строка")
	}
}

func TestContinuationRequestKeepsPartialAndNudge(t *testing.T) {
	req := &llm.ChatRequest{Model: "m", Messages: []llm.Message{{Role: "user", Content: "вопрос"}}}
	partial := respWith("начало ответа на полусл", "рассуждение")

	got := continuationRequest(req, partial)

	if len(got.Messages) != 3 {
		t.Fatalf("ожидали 3 сообщения, получили %d", len(got.Messages))
	}
	if got.Messages[1].Role != "assistant" || got.Messages[1].Content != "начало ответа на полусл" {
		t.Fatalf("частичный ответ не подставлен: %+v", got.Messages[1])
	}
	if got.Messages[1].ReasoningContent != "рассуждение" {
		t.Fatal("reasoning_content должен сохраняться (DeepSeek требует round-trip)")
	}
	if got.Messages[2].Role != "user" || got.Messages[2].Content == "" {
		t.Fatal("нужен user-nudge «допиши хвост»")
	}
	if len(req.Messages) != 1 {
		t.Fatal("исходный запрос не должен изменяться")
	}
}

func TestMergeContinuationConcatenates(t *testing.T) {
	got := mergeContinuation(respWith("начало ", "дум1"), respWith("и конец.", "дум2"))
	if got.Choices[0].Message.Content != "начало и конец." {
		t.Fatalf("контент: %q", got.Choices[0].Message.Content)
	}
	if got.Choices[0].Message.ReasoningContent != "дум1дум2" {
		t.Fatalf("reasoning: %q", got.Choices[0].Message.ReasoningContent)
	}
}
