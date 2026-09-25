package agent

import (
	"strings"
	"testing"
)

// Предупреждение должно появляться для действительно долгих операций, а не для
// любой команды с большим timeout_sec («на всякий случай» его ставят часто).
func TestLongStepNotice(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"короткая команда с запасом по времени молчит", `{"command":"git status","timeout_sec":300}`, ""},
		{"git push молчит", `{"command":"git push home master", "explanation":"Push в home-remote", "timeout_sec":300}`, ""},
		{"уборка файлов молчит", `{"command":"Remove-Item -Recurse build\\tmp","timeout_sec":180}`, ""},
		{"сборка с запасом", `{"command":"wails build -platform windows/amd64","timeout_sec":900,"explanation":"Сборка релиза"}`, "Сборка релиза"},
		{"тесты", `{"command":"go test ./...","timeout_sec":600}`, "go test"},
		{"установка зависимостей", `{"command":"npm ci","timeout_sec":300}`, "npm ci"},
		{"сборка векторов по-русски", `{"command":"Start-Sleep -Seconds 290","explanation":"Ожидание сборки векторов","timeout_sec":330}`, "Ожидание сборки векторов"},
		{"долгая операция, но лимит маленький", `{"command":"go test ./...","timeout_sec":30}`, ""},
		{"фон всегда долгий", `{"command":"python server.py","is_background":true,"timeout_sec":60}`, "python server.py"},
		{"мусор в аргументах молчит", `не json`, ""},
	}
	for _, c := range cases {
		got := longStepNotice("run_terminal", c.args)
		if c.want == "" {
			if got != "" {
				t.Errorf("%s: ожидали молчание, получили %q", c.name, got)
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
	if !strings.Contains(got, "30 мин") || !strings.Contains(got, "в фоне") {
		t.Errorf("фон без лимита: %q", got)
	}
	// Не run_terminal — молчим.
	if note := longStepNotice("read_file", `{"timeout_sec":600}`); note != "" {
		t.Errorf("read_file не должен получать предупреждение: %q", note)
	}
}

func TestLooksLikeLongWork(t *testing.T) {
	long := []string{"go build ./...", "docker compose up -d", "pip install onnxruntime", "python embed.py --all", "cargo build --release", "Сборка индекса"}
	for _, s := range long {
		if !looksLikeLongWork(s, "") {
			t.Errorf("%q должно считаться долгой операцией", s)
		}
	}
	short := []string{"git push", "git commit -m x", "ls", "Get-Content README.md", "gh release upload v0.7.6 file.zip", "start chrome"}
	for _, s := range short {
		if looksLikeLongWork(s, "") {
			t.Errorf("%q не должно считаться долгой операцией", s)
		}
	}
	// Признак может быть в объяснении, а не в команде.
	if !looksLikeLongWork("Start-Sleep -Seconds 290", "Ожидание сборки векторов") {
		t.Error("объяснение должно учитываться")
	}
}
