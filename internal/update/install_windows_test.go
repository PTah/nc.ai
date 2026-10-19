// Edited by nc.ai 0.8.8.5 on new-admin-pc 2026-10-19 11:40:05
//go:build windows

package update

import (
	"os"
	"path/filepath"
	"testing"
)

// TestApplyUpdateReplacesInstalledExe: helper самоприменения должен подменить
// exe по указанному пути и оставить лог. Раньше подмена шла через PowerShell;
// теперь — тот же бинарь с --apply-update, поэтому проверяем applyUpdate напрямую.
func TestApplyUpdateReplacesInstalledExe(t *testing.T) {
	stage, err := os.MkdirTemp("", "nc-applytest-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage)

	newExe := filepath.Join(stage, "new.exe")
	if err := os.WriteFile(newExe, []byte("NEW BINARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(stage, "installed")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appExe := filepath.Join(appDir, "NotCursor.exe")
	if err := os.WriteFile(appExe, []byte("OLD BINARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(stage, "apply.log")

	// waitPid=0 — не ждём процесс; перезапуск «битого» exe просто не удастся,
	// но файл обязан быть подменён.
	if code := applyUpdate(0, newExe, appExe, logPath); code != 0 {
		t.Fatalf("applyUpdate code=%d", code)
	}
	got, _ := os.ReadFile(appExe)
	if string(got) != "NEW BINARY" {
		t.Fatalf("exe not replaced: %q", got)
	}
	if fi, err := os.Stat(logPath); err != nil || fi.Size() == 0 {
		t.Fatalf("apply.log missing/empty: %v", err)
	}
	if _, err := os.Stat(appExe + ".old"); err == nil {
		t.Fatalf(".old should be removed after a successful replace")
	}
}

// TestApplyUpdateIgnoresNormalArgs: обычный запуск не должен включать режим.
func TestApplyUpdateIgnoresNormalArgs(t *testing.T) {
	if ApplyUpdate([]string{"--some", "flag"}) {
		t.Fatal("ApplyUpdate must return false for non-apply args")
	}
}
