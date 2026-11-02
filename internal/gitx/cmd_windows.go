// Edited by nc.ai 1.0.7 on new-admin-pc 2026-11-02 10:42:55
//go:build windows

package gitx

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// ConfigureCmd — экспорт для фоновых вызовов git вне пакета: без консольного окна.
func ConfigureCmd(cmd *exec.Cmd) {
	configureCmd(cmd)
}
