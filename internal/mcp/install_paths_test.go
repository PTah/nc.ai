// Created by nc.ai 0.8.5.3 on macbook 2026-10-12 21:49:38
package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSearchBinDirsFindsExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mytool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := searchBinDirs("mytool", []string{"/nope", dir, t.TempDir()})
	if err != nil {
		t.Fatalf("searchBinDirs: %v", err)
	}
	if got != filepath.Join(dir, "mytool") {
		t.Fatalf("нашли %q, хотели файл в %q", got, dir)
	}
	if _, err := searchBinDirs("mytool", []string{"/nope"}); err == nil {
		t.Fatal("отсутствующий файл должен давать ошибку")
	}
	// Пустые каталоги в списке не должны ломать поиск.
	if _, err := searchBinDirs("mytool", []string{"", "  "}); err == nil {
		t.Fatal("пустые каталоги — тоже «не найдено»")
	}
}

// Приложение из Finder не видит Homebrew-каталоги в PATH — они обязаны быть
// среди кандидатов, иначе «npm не найден», хотя Node установлен.
func TestCandidateBinDirsHomebrewOnDarwin(t *testing.T) {
	dirs := candidateBinDirs()
	has := func(want string) bool {
		for _, d := range dirs {
			if d == want {
				return true
			}
		}
		return false
	}
	if runtime.GOOS == "darwin" && !has("/opt/homebrew/bin") {
		t.Fatalf("на macOS среди каталогов поиска нет /opt/homebrew/bin: %v", dirs)
	}
	if !has(filepath.Join(mustHome(t), ".local", "bin")) {
		t.Fatalf("нет ~/.local/bin в списке: %v", dirs)
	}
}

// PATH для MCP-серверов: сохраняем исходные записи и добавляем существующие
// каталоги пакетных менеджеров (Node/Python-обёртки ищут интерпретатор в PATH).
func TestWithBinDirsInPath(t *testing.T) {
	env := withBinDirsInPath([]string{"PATH=/usr/bin:/bin", "HOME=/tmp"})
	path := ""
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			path = kv[len("PATH="):]
		}
	}
	if path == "" {
		t.Fatal("PATH потерялся")
	}
	parts := strings.Split(path, string(os.PathListSeparator))
	has := func(want string) bool {
		for _, p := range parts {
			if p == want {
				return true
			}
		}
		return false
	}
	if !has("/usr/bin") || !has("/bin") {
		t.Fatalf("исходный PATH не сохранён: %q", path)
	}
	for _, dir := range candidateBinDirs() {
		if st, err := os.Stat(dir); err == nil && st.IsDir() && !has(dir) {
			t.Fatalf("существующий каталог %q не попал в PATH: %q", dir, path)
		}
	}
	if n := strings.Count(path, "/usr/bin"); n > 1 {
		t.Fatalf("PATH содержит дубли: %q", path)
	}
}

func mustHome(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return home
}
