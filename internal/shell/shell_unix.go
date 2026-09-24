//go:build !windows

package shell

import "os/exec"

// ConfigureCmd is a no-op outside Windows (no console window to hide).
func ConfigureCmd(cmd *exec.Cmd) {}

func decodeShellBytes(b []byte) string {
	return string(b)
}
