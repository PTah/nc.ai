// Created by nc.ai 1.2.15 on Papasha 2026-11-21 11:07:05
package mcp

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestTreeGuardCloseIsIdempotent locks down the contract that made the "dead
// session" leak possible: the guard is released both by readLoop (server died on
// its own) and by Close, so the second release must be a harmless no-op instead
// of a double handle close or a kill aimed at a reused pid.
func TestTreeGuardCloseIsIdempotent(t *testing.T) {
	cmd := exec.Command(goBin(), "version")
	guard, err := newTreeGuard(cmd)
	if err != nil {
		t.Fatalf("newTreeGuard: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := guard.adopt(cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		t.Fatalf("adopt: %v", err)
	}
	guard.close()
	guard.close()
	_, _ = cmd.Process.Wait()
}

// TestRunCmdOutUsesTreeGuard: package installers used to be the only spawn path
// without a job object / process group (only CREATE_NO_WINDOW), so a timed-out
// `npm install -g` left its node children behind. The smoke test also proves the
// suspended start is resumed — otherwise the command would hang here forever.
func TestRunCmdOutUsesTreeGuard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	bin, args := "sh", []string{"-c", "echo install-ok"}
	if runtime.GOOS == "windows" {
		bin, args = "cmd.exe", []string{"/c", "echo install-ok"}
	}
	out, err := runCmdOut(ctx, bin, args, nil)
	if err != nil {
		t.Fatalf("runCmdOut: %v (%s)", err, out)
	}
	if !strings.Contains(out, "install-ok") {
		t.Fatalf("out=%q", out)
	}
}
