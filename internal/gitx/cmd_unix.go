//go:build !windows

package gitx

import (
	"os/exec"
	"syscall"
)

// configureCmd: отдельная process group, чтобы CancelClone убивал и git-remote-https,
// а не только родительский `git clone` (иначе хвост продолжает качать pack).
func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Минус PID = сигнал всей группе.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
