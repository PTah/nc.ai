package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Границы проекта: чужие проекты и выход из рабочей папки требуют подтверждения,
// а свои файлы, временные и системные каталоги — нет.
func TestCheckCommandScope(t *testing.T) {
	// Пути синтетические: проверяем арифметику путей, а не файловую систему.
	// С диском в начале — как в реальных командах агента (D:\Soft\Git\...).
	wd, _ := os.Getwd()
	base := filepath.Join(filepath.VolumeName(wd)+string(filepath.Separator), "ncai-scope-test")
	root := filepath.Join(base, "project")
	other := filepath.Join(base, "other-project")
	elsewhere := filepath.Join(base, "downloads")

	cases := []struct {
		name    string
		command string
		want    string // "" — подтверждение не нужно
		what    string
	}{
		{"своя папка", `python intel/embed.py --all`, "", ""},
		{"свой файл по абсолютному пути", "Get-Content " + filepath.Join(root, "src", "main.go"), "", ""},
		{"git в своём репозитории", `git push home master`, "", ""},
		{"чужой проект по абсолютному пути", `git -C ` + other + ` pull --ff-only`, "другой проект", other},
		{"чужой проект через cd", `cd ` + other + ` ; git pull`, "другой проект", other},
		{"выход за пределы проекта", `cd ` + elsewhere + ` ; Remove-Item -Recurse .`, "вне проекта", elsewhere},
		{"относительный выход", `Get-Content ..\..\secret.txt`, "вне проекта", ""},
		{"домашний каталог", `Get-Content ~/.ssh/id_ed25519`, "вне проекта", ""},
		{"профиль через переменную", `type $env:USERPROFILE\.gitconfig`, "вне проекта", ""},
		{"временный каталог можно", `Expand-Archive $env:TEMP\x.zip`, "", ""},
		{"системные пути можно", `Get-Content C:\Windows\System32\drivers\etc\hosts`, "", ""},
		{"не команда терминала", `нет-парсинга`, "", ""},
	}
	for _, c := range cases {
		got := checkCommandScope(root, []string{other}, root, c.command)
		if c.want == "" {
			if got != nil {
				t.Errorf("%s: ожидали разрешение, получили %+v", c.name, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("%s: ожидали причину %q, получили nil", c.name, c.want)
			continue
		}
		if got.Reason != c.want {
			t.Errorf("%s: причина %q, ожидали %q", c.name, got.Reason, c.want)
		}
		if c.what != "" && !strings.EqualFold(got.What, c.what) {
			t.Errorf("%s: путь %q, ожидали %q", c.name, got.What, c.what)
		}
	}
}

// Свой проект упоминать можно, даже если это подпапка; временный каталог — тоже.
func TestCheckCommandScopeOwnSubdir(t *testing.T) {
	wd, _ := os.Getwd()
	base := filepath.Join(filepath.VolumeName(wd)+string(filepath.Separator), "ncai-scope-test")
	root := filepath.Join(base, "project")
	other := filepath.Join(base, "other-project")
	if got := checkCommandScope(root, nil, root, `go test ./internal/...`); got != nil {
		t.Fatalf("свой подкаталог: %+v", got)
	}
	if got := checkCommandScope(root, nil, filepath.Join(root, "frontend"), `npm run build`); got != nil {
		t.Fatalf("cwd внутри проекта: %+v", got)
	}
	if got := checkCommandScope(root, nil, root, `python -m venv `+filepath.Join(os.TempDir(), "venv")); got != nil {
		t.Fatalf("временный каталог должен быть разрешён: %+v", got)
	}
	// cwd в другом проекте — спрашиваем, даже если в команде путей нет.
	got := checkCommandScope(root, []string{other}, other, `git pull`)
	if got == nil || got.Reason != "другой проект" {
		t.Fatalf("cwd чужого проекта: %+v", got)
	}
}

// Текст для диалога подтверждения должен объяснять, что случилось.
func TestScopeIssueText(t *testing.T) {
	issue := &ScopeIssue{Reason: "другой проект", What: `D:\Soft\Git\Pentest`}
	if text := issue.Text(); !strings.Contains(text, "другой проект") || !strings.Contains(text, "Pentest") {
		t.Fatalf("text=%q", text)
	}
	issue = &ScopeIssue{Reason: "вне проекта", What: `D:\Downloads`}
	if text := issue.Text(); !strings.Contains(text, "за пределы проекта") {
		t.Fatalf("text=%q", text)
	}
	var nilIssue *ScopeIssue
	if nilIssue.Text() != "" {
		t.Fatal("nil-issue должен давать пустой текст")
	}
}
