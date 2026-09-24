package llm

import (
	"io"
	"strings"
	"testing"
	"time"
)

// Живое, но замолчавшее соединение не должно висеть: именно на этом шаг
// «застывал» на 12 минут без единого события (см. docs/stall-2026-09-24.md).
func TestConsumeOpenAISSEIdleTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	go func() {
		_, _ = io.WriteString(pw, "data: {\"choices\":[{\"delta\":{\"content\":\"привет\"}}]}\n")
		// дальше тишина: соединение живо, данных нет
	}()

	start := time.Now()
	_, err := consumeOpenAISSE(pr, nil, 300*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("ожидалась ошибка про тишину в потоке, получено nil")
	}
	if !strings.Contains(err.Error(), "stream idle") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("idle-лимит не сработал вовремя: %s", elapsed)
	}
}

// Нормальный поток с [DONE] завершается успешно и собирает текст/рассуждения.
func TestConsumeOpenAISSECollectsDeltas(t *testing.T) {
	const body = "data: {\"id\":\"x\",\"model\":\"m\",\"choices\":[{\"delta\":{\"content\":\"от\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"вет\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"думал\"}}]}\n" +
		"data: [DONE]\n"

	res, err := consumeOpenAISSE(strings.NewReader(body), nil, time.Second)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if got := res.Choices[0].Message.Content; got != "ответ" {
		t.Errorf("контент %q, ожидался %q", got, "ответ")
	}
	if got := res.Choices[0].Message.ReasoningContent; got != "думал" {
		t.Errorf("рассуждения %q, ожидались %q", got, "думал")
	}
	if res.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason %q, ожидался stop", res.Choices[0].FinishReason)
	}
}

// Обрыв чтения должен возвращать ошибку чтения, а не тишину и не пустой ответ.
func TestConsumeOpenAISSEReadError(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n")
		_ = pw.CloseWithError(io.ErrUnexpectedEOF)
	}()

	_, err := consumeOpenAISSE(pr, nil, 5*time.Second)
	if err == nil {
		t.Fatal("ожидалась ошибка чтения потока")
	}
	if !strings.Contains(err.Error(), "stream read") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}
