//go:build !windows

package shell

import (
	"os/exec"
	"syscall"
)

// ConfigureCmd is a no-op outside Windows (no console window to hide).
func ConfigureCmd(cmd *exec.Cmd) {}

// prepareTree сажает команду в отдельную группу процессов: так её можно погасить
// целиком (иначе дети — ssh, docker, серверы — остаются жить после таймаута).
func prepareTree(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree убивает всю группу процессов команды.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}

func decodeShellBytes(b []byte) string {
	return string(b)
}
