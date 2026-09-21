//go:build windows

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestSwapReplacesInstalledExe is the regression test for "скачал обновление, но
// ничего не подменилось": the helper must really execute (swap.log) and replace
// the exe it was pointed at. It used to die before its first line because the
// helper was spawned with DETACHED_PROCESS (PowerShell 5.1 refuses to run).
func TestSwapReplacesInstalledExe(t *testing.T) {
	stage, err := os.MkdirTemp("", "nc-swaptest-")
	if err != nil {
		t.Fatal(err)
	}
	// The helper keeps swap.err open until it exits, so retry the cleanup.
	defer func() {
		for i := 0; i < 30; i++ {
			if err := os.RemoveAll(stage); err == nil {
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
	}()
	unpacked := filepath.Join(stage, "new")
	if err := os.MkdirAll(unpacked, 0o755); err != nil {
		t.Fatal(err)
	}
	newExe := filepath.Join(unpacked, "NotCursor.exe")
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

	// Stand-in for the application process the helper has to wait for.
	dummy := exec.Command("powershell.exe", "-NoProfile", "-Command", "Start-Sleep -Seconds 4")
	if err := dummy.Start(); err != nil {
		t.Fatalf("dummy app: %v", err)
	}
	defer func() { _ = dummy.Process.Kill() }()

	if err := spawnSwapFor(stage, unpacked, appExe, dummy.Process.Pid); err != nil {
		t.Fatalf("spawnSwapFor: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(appExe); err == nil && string(data) == "NEW BINARY" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	got, _ := os.ReadFile(appExe)
	logData, _ := os.ReadFile(filepath.Join(stage, "swap.log"))
	errData, _ := os.ReadFile(filepath.Join(stage, "swap.err"))
	if string(got) != "NEW BINARY" {
		t.Fatalf("exe not replaced: %q\nswap.log: %s\nswap.err: %s", got, logData, errData)
	}
	if len(logData) == 0 {
		t.Fatalf("swap.log is empty: the helper never reached its first step")
	}
	t.Logf("swap.log:\n%s", logData)
}
