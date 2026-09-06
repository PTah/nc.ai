//go:build !windows

package shell

import "os/exec"

func configureCmd(cmd *exec.Cmd) {}

func decodeShellBytes(b []byte) string {
	return string(b)
}
