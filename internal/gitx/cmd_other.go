// Created by nc.ai 1.0.7 on new-admin-pc 2026-11-02 10:42:55
//go:build !windows

package gitx

import "os/exec"

// ConfigureCmd — на Unix консольного окна нет, ничего не делаем.
func ConfigureCmd(cmd *exec.Cmd) {}
