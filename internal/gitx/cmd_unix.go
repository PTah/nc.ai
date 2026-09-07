//go:build !windows

package gitx

import "os/exec"

func configureCmd(cmd *exec.Cmd) {}
