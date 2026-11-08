// Created by nc.ai 1.1.8 on macbook 2026-11-08 20:53:32
package gitx

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip собирает zip с заданными файлами (имя → содержимое).
func writeZip(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	// Детерминированный порядок: map перебирается случайно, а тесты проверяют лимиты.
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip entry %s: %v", name, err)
		}
		if _, err := fw.Write(files[name]); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("zip file close: %v", err)
	}
}

// Распаковка не должна вылезать за пределы целевой папки, даже если в архиве
// лежит «../evil.txt»: запись просто пропускается.
func TestUnzipFlatSkipsTraversal(t *testing.T) {
	base := t.TempDir()
	zipPath := filepath.Join(base, "bomb.zip")
	writeZip(t, zipPath, map[string][]byte{
		"repo-abc/ok.txt": []byte("ok"),
		"../evil.txt":     []byte("evil"),
		"repo-abc/x/../y": []byte("weird"),
	})
	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	top, err := unzipFlat(zipPath, dest, nil)
	if err != nil {
		t.Fatalf("unzipFlat: %v", err)
	}
	if !strings.HasSuffix(top, "repo-abc") {
		t.Errorf("top=%q, ожидали папку repo-abc", top)
	}
	if _, err := os.Stat(filepath.Join(base, "evil.txt")); err == nil {
		t.Error("файл из ../ попал за пределы dest")
	}
}

// Сжатый архив может развернуться в гигабайты: лимит распаковки обязан сработать.
func TestUnzipFlatEnforcesUnpackedLimit(t *testing.T) {
	prev := maxUnpackedBytes
	maxUnpackedBytes = 64 << 10
	t.Cleanup(func() { maxUnpackedBytes = prev })

	base := t.TempDir()
	zipPath := filepath.Join(base, "big.zip")
	writeZip(t, zipPath, map[string][]byte{
		"repo-abc/big.bin": make([]byte, 200<<10), // 200 КБ нулей — сжимается в копейки
	})
	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := unzipFlat(zipPath, dest, nil); err == nil {
		t.Fatal("ожидали ошибку лимита распаковки")
	} else if !strings.Contains(err.Error(), "лимит") {
		t.Fatalf("неожидаемая ошибка: %v", err)
	}
}

// Слишком много файлов в архиве — тоже отказ, а не сотни тысяч inode.
func TestUnzipFlatEnforcesEntryLimit(t *testing.T) {
	prev := maxZipballEntries
	maxZipballEntries = 3
	t.Cleanup(func() { maxZipballEntries = prev })

	base := t.TempDir()
	zipPath := filepath.Join(base, "many.zip")
	writeZip(t, zipPath, map[string][]byte{
		"repo-abc/a": []byte("a"),
		"repo-abc/b": []byte("b"),
		"repo-abc/c": []byte("c"),
		"repo-abc/d": []byte("d"),
	})
	dest := filepath.Join(base, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := unzipFlat(zipPath, dest, nil); err == nil {
		t.Fatal("ожидали ошибку лимита количества файлов")
	} else if !strings.Contains(err.Error(), "лимит") {
		t.Fatalf("неожидаемая ошибка: %v", err)
	}
}

// Цель клонирования — существующий файл: раньше он молча удалялся (os.RemoveAll),
// теперь Clone обязан отказать до любых удалений.
func TestCloneRejectsFileTarget(t *testing.T) {
	base := t.TempDir()
	origin := filepath.Join(base, "origin")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(work, "origin")
	if err := os.WriteFile(target, []byte("не удаляй меня"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := New(nil).Clone(context.Background(), origin, work)
	if err == nil {
		t.Fatal("ожидали отказ: цель — файл")
	}
	if !strings.Contains(err.Error(), "файл") {
		t.Fatalf("неожидаемая ошибка: %v", err)
	}
	if data, rerr := os.ReadFile(target); rerr != nil || string(data) != "не удаляй меня" {
		t.Fatalf("файл цели пострадал: %q, %v", data, rerr)
	}
}

// Windows-путь должен распознаваться как локальный на любой ОС: пользователь может
// вставить его на маке, а тест на Windows-машине проверяет ровно это.
func TestIsWindowsAbsPath(t *testing.T) {
	for _, ok := range []string{`D:\repos\origin`, `d:/repos/origin`, `C:\x`} {
		if !isWindowsAbsPath(ok) {
			t.Errorf("isWindowsAbsPath(%q) = false, ожидали true", ok)
		}
	}
	for _, bad := range []string{``, `D:`, `D:repos`, `1:\x`, `\\server\share`, `/unix/path`} {
		if isWindowsAbsPath(bad) {
			t.Errorf("isWindowsAbsPath(%q) = true, ожидали false", bad)
		}
	}
}
