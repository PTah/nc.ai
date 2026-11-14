// Created by nc.ai 1.2.13 on Papasha 2026-11-14 22:12:35
//go:build !windows

// Created by nc.ai 1.2.13 on Papasha 2026-11-14 22:34:00
package mcp

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestTreeGuardSetsProcessGroup checks the Unix half of the tree guard: the
// child must lead its own process group, otherwise Session.Close (which signals
// -pid) cannot reach grandchildren such as the real server behind an npx/uvx
// wrapper.
func TestTreeGuardSetsProcessGroup(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30")
	guard, err := newTreeGuard(cmd)
	if err != nil {
		t.Fatalf("newTreeGuard: %v", err)
	}
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Fatal("Setpgid is not set: grandchildren would survive Close")
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
	// The group kill above must leave the child reaped by us, not hanging.
	_, _ = cmd.Process.Wait()
	if err := syscall.Kill(-cmd.Process.Pid, 0); err == nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		t.Fatal("process group still exists after tree guard close")
	}
	guard.close() // idempotent
}
