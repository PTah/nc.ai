// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:29:05
// Created by nc.ai 1.1.16 on macbook 2026-11-11
package llm

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// Обрыв SSE должен говорить, сколько секунд поток молчал и сколько строк успел
// отдать: иначе «connection reset» не отличить от «сервер молчал и упал».
func TestConsumeOpenAISSE_ReadErrorReportsSilence(t *testing.T) {
	body := `data: {"choices":[{"delta":{"content":"при"}}]}` + "\n" +
		`data: {"choices":[{"delta":{"content":"вет"}}]}` + "\n"
	r := io.MultiReader(strings.NewReader(body), iotest.ErrReader(errors.New("connection reset by peer")))

	if _, err := ConsumeOpenAISSE(r, nil); err == nil {
		t.Fatal("ожидалась ошибка чтения потока, получено nil")
	} else {
		msg := err.Error()
		for _, want := range []string{"stream read", "обрыв после", "строк получено: 2", "connection reset"} {
			if !strings.Contains(msg, want) {
				t.Fatalf("в сообщении нет %q: %s", want, msg)
			}
		}
	}
}

// Обрыв до первого события тоже должен попадать в диагностику (тишина от старта).
func TestConsumeOpenAISSE_ReadErrorBeforeAnyLine(t *testing.T) {
	r := iotest.ErrReader(errors.New("unexpected EOF"))
	_, err := ConsumeOpenAISSE(r, nil)
	if err == nil {
		t.Fatal("ожидалась ошибка чтения потока, получено nil")
	}
	if !strings.Contains(err.Error(), "строк получено: 0") {
		t.Fatalf("ожидалось «строк получено: 0»: %s", err.Error())
	}
}
