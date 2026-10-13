// Created by nc.ai 0.8.5.4 on new-admin-pc 2026-10-13 10:03:06
//go:build windows

package mcp

import (
	"os/exec"
	"syscall"
)

// configureProcess prevents a console window from popping up for child
// processes. NotCursor is a GUI-subsystem binary with no console of its own, so
// any console child (node.exe, or cmd.exe running a .cmd shim like npm.cmd /
// codebase-memory-mcp.cmd) would otherwise get a brand-new cmd window. Setting
// CREATE_NO_WINDOW + HideWindow keeps MCP servers and installers in the
// background.
func configureProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
