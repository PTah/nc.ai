package agent

import (
	"strings"
	"testing"
)

// Долгие команды помечаем заранее: пользователь должен видеть «это минуты»,
// даже если модель не сказала об этом в чате.
func TestLongStepNotice(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"короткая команда молчит", `{"command":"go build ./...","timeout_sec":60}`, ""},
		{"без таймаута молчит", `{"command":"ls"}`, ""},
		{"три минуты", `{"command":"go test ./...", "timeout_sec":180, "explanation":"прогон тестов"}`, "Долгий шаг: прогон тестов"},
		{"ровно порог", `{"command":"npm ci", "timeout_sec":120}`, "Долгий шаг: npm ci"},
		{"фон", `{"command":"python embed.py --all", "is_background":true, "explanation":"сборка векторов"}`, "сборка векторов"},
		{"мусор в аргументах", `не json`, ""},
	}
	for _, c := range cases {
		got := longStepNotice("run_terminal", c.args)
		if c.want == "" {
			if got != "" {
				t.Errorf("%s: ожидали пусто, получили %q", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: %q не содержит %q", c.name, got, c.want)
		}
		if !strings.Contains(got, "лимит до") {
			t.Errorf("%s: в предупреждении нет лимита: %q", c.name, got)
		}
	}
	// Фоновая задача без явного лимита — говорим про 30 минут по умолчанию.
	got := longStepNotice("run_terminal", `{"command":"server", "is_background":true}`)
	if !strings.Contains(got, "30 мин") {
		t.Errorf("фон без лимита: %q", got)
	}
	// Не run_terminal — молчим.
	if note := longStepNotice("read_file", `{"timeout_sec":600}`); note != "" {
		t.Errorf("read_file не должен получать предупреждение: %q", note)
	}
}
