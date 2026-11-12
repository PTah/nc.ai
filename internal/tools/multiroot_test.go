// Edited by nc.ai 1.2.0 on new-admin-pc 2026-11-12 11:08:00
package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"notcursor.ai/app/internal/projmem"
	"notcursor.ai/app/internal/workspace"
)

// newMultiRegistry — рабочее пространство из двух папок: api и web.
func newMultiRegistry(t *testing.T) (*Registry, string, string) {
	t.Helper()
	key := t.TempDir()
	api := filepath.Join(t.TempDir(), "api")
	web := filepath.Join(t.TempDir(), "web")
	for _, d := range []string{api, web} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ws := workspace.NewManager()
	if _, err := ws.UpsertWorkspace(key, "ws", []string{api, web}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Open(key); err != nil {
		t.Fatal(err)
	}
	return NewRegistry(ws, "").ForRun(key), api, web
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git не найден")
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestMultiRootTerminalNeedsCwd(t *testing.T) {
	run, _, _ := newMultiRegistry(t)
	ctx := context.Background()

	_, err := run.Execute(ctx, callFor("run_terminal", `{"command":"echo hi"}`))
	if err == nil || !strings.Contains(err.Error(), "api") || !strings.Contains(err.Error(), "web") {
		t.Fatalf("без cwd ждём ошибку с перечнем папок, err=%v", err)
	}
	out, err := run.Execute(ctx, callFor("run_terminal", `{"command":"echo hi","cwd":"api"}`))
	if err != nil || !strings.Contains(out, "exit=0") {
		t.Fatalf("cwd=api: %q, %v", out, err)
	}
}

func TestMultiRootScopeKnowsAllFolders(t *testing.T) {
	run, _, web := newMultiRegistry(t)
	if issue := run.CommandScopeIssue("run_terminal", `{"command":"go test ./...","cwd":"web"}`); issue != nil {
		t.Fatalf("cwd во второй папке пространства не должен требовать подтверждения: %+v", issue)
	}
	cmd := `{"command":"cat ` + filepath.ToSlash(filepath.Join(web, "a.txt")) + `","cwd":"api"}`
	if issue := run.CommandScopeIssue("run_terminal", cmd); issue != nil {
		t.Fatalf("путь в соседней папке пространства — не «вне проекта»: %+v", issue)
	}
	cmd = `{"command":"echo x > ` + outsidePath(t) + `","cwd":"api"}`
	if issue := run.CommandScopeIssue("run_terminal", cmd); issue == nil {
		t.Fatal("путь вне пространства должен требовать подтверждения")
	}
}

// outsidePath — абсолютный путь, заведомо лежащий вне корней пространства, на
// любой ОС: на Windows `/usr/local` не считается абсолютным, поэтому тест раньше
// падал именно там.
func outsidePath(t *testing.T) string {
	if runtime.GOOS == "windows" {
		return filepath.ToSlash(filepath.Join(os.TempDir(), "nc-ai-outside.txt"))
	}
	return "/usr/local/nc-ai-outside.txt"
}

func TestMultiRootFileToolsUseFolderNames(t *testing.T) {
	run, api, web := newMultiRegistry(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(api, "main.go"), []byte("package main\n\nfunc Alpha() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "app.go"), []byte("package web\n\nfunc Beta() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run.Execute(ctx, callFor("find_symbol", `{"name":"Alpha"}`))
	if err != nil || !strings.Contains(out, "api/main.go") {
		t.Fatalf("find_symbol Alpha = %q, %v", out, err)
	}
	out, err = run.Execute(ctx, callFor("find_symbol", `{"name":"Beta"}`))
	if err != nil || !strings.Contains(out, "web/app.go") {
		t.Fatalf("find_symbol Beta = %q, %v", out, err)
	}
	out, err = run.Execute(ctx, callFor("find_symbol", `{"name":"Beta","path":"api"}`))
	if err != nil || strings.Contains(out, "web/app.go") {
		t.Fatalf("path=api не должен находить символы web: %q, %v", out, err)
	}
	out, err = run.Execute(ctx, callFor("get_env_info", `{}`))
	if err != nil || !strings.Contains(out, "workspace_folders=") || !strings.Contains(out, "api=") || !strings.Contains(out, "web=") {
		t.Fatalf("env = %q, %v", out, err)
	}
}

func TestMultiRootGitPerRepo(t *testing.T) {
	run, api, web := newMultiRegistry(t)
	gitInit(t, api)
	gitInit(t, web)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(api, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "w.txt"), []byte("w"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := run.Execute(ctx, callFor("git_status", `{}`))
	if err != nil || !strings.Contains(out, "## api") || !strings.Contains(out, "## web") ||
		!strings.Contains(out, "a.txt") || !strings.Contains(out, "w.txt") {
		t.Fatalf("git_status по всем папкам = %q, %v", out, err)
	}
	out, err = run.Execute(ctx, callFor("git_status", `{"repo":"web"}`))
	if err != nil || strings.Contains(out, "a.txt") || !strings.Contains(out, "w.txt") {
		t.Fatalf("git_status repo=web = %q, %v", out, err)
	}

	// Коммит без repo и без путей неоднозначен.
	if _, err := run.Execute(ctx, callFor("git_commit", `{"message":"x"}`)); err == nil {
		t.Fatal("git_commit без repo в мульти-пространстве должен просить repo")
	}
	// Репозиторий выводится из префикса пути, префикс срезается.
	out, err = run.Execute(ctx, callFor("git_commit", `{"message":"add a","paths":["api/a.txt"]}`))
	if err != nil {
		t.Fatalf("git_commit api/a.txt: %v\n%s", err, out)
	}
	apiLog, _ := exec.Command("git", "-C", api, "log", "--oneline").CombinedOutput()
	if !strings.Contains(string(apiLog), "add a") {
		t.Fatalf("коммит не попал в api: %s", apiLog)
	}
	if webLog, _ := exec.Command("git", "-C", web, "log", "--oneline").CombinedOutput(); strings.Contains(string(webLog), "add a") {
		t.Fatalf("коммит попал не в тот репозиторий: %s", webLog)
	}
	// Пути из разных папок одним коммитом — ошибка.
	if _, err := run.Execute(ctx, callFor("git_commit", `{"message":"both","paths":["api/a.txt","web/w.txt"]}`)); err == nil {
		t.Fatal("коммит с путями из двух папок должен быть отклонён")
	}
	// Неизвестная папка.
	if _, err := run.Execute(ctx, callFor("git_log", `{"repo":"nope"}`)); err == nil || !strings.Contains(err.Error(), "api") {
		t.Fatalf("git_log repo=nope: err=%v", err)
	}
}

// Память хранится по ключу пространства, а пути заметок «api/main.go» разворачиваются
// по папкам: правка файла в любой папке помечает заметку устаревшей.
func TestMultiRootMemoryResolverMarksStale(t *testing.T) {
	run, api, _ := newMultiRegistry(t)
	file := filepath.Join(api, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := run.chatProjectKey()
	if err != nil {
		t.Fatal(err)
	}
	st := projmem.New("") // только в памяти: настоящее хранилище пользователя не трогаем
	n, err := st.Remember(key, "api владеет main", []string{"api/main.go"})
	if err != nil {
		t.Fatal(err)
	}
	if got := projmem.FormatNotesWith(key, []projmem.Note{n}, run.memoryResolver()); strings.Contains(got, "stale") {
		t.Fatalf("свежая заметка помечена устаревшей: %q", got)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(file, future, future); err != nil {
		t.Fatal(err)
	}
	if got := projmem.FormatNotesWith(key, []projmem.Note{n}, run.memoryResolver()); !strings.Contains(got, "stale") {
		t.Fatalf("после правки файла в папке api заметка должна устареть: %q", got)
	}
}
