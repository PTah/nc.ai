// Created by nc.ai 0.8.5.4 on new-admin-pc 2026-10-13 10:03:06
//go:build !windows

package mcp

import "os/exec"

// configureProcess is a no-op off Windows (no console-window problem there).
func configureProcess(cmd *exec.Cmd) {}
