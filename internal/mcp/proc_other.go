// Edited by nc.ai 1.2.13 on Papasha 2026-11-14 22:55:00
// Created by nc.ai 0.8.5.4 on new-admin-pc 2026-10-13 10:03:06
// Edited by nc.ai 1.2.12 on Papasha 2026-11-14 21:54:32
//go:build !windows

package mcp

import (
	"os/exec"
	"sync"
	"syscall"
)

// treeGuard on Unix puts the child in its own process group (Setpgid) and, on
// Close, signals the whole group with SIGKILL. Session.Close already kills the
// direct child; the group kill catches grandchildren (e.g. an npx/uvx wrapper
// that spawned the real server), which would otherwise outlive the session.
type treeGuard struct {
	mu  sync.Mutex
	pid int
}

func newTreeGuard(cmd *exec.Cmd) (*treeGuard, error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return &treeGuard{}, nil
}

func (g *treeGuard) adopt(pid int) error {
	g.mu.Lock()
	g.pid = pid
	g.mu.Unlock()
	return nil
}

// close kills the whole process group. Safe to call more than once and from
// several goroutines: the session releases the group both on Close and when the
// server process dies on its own.
func (g *treeGuard) close() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pid <= 0 {
		return
	}
	_ = syscall.Kill(-g.pid, syscall.SIGKILL)
	g.pid = 0
}
