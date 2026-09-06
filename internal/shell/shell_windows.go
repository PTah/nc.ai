//go:build windows

package shell

import (
	"os/exec"
	"syscall"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const createNoWindow = 0x08000000

func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

func decodeShellBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	// Russian Windows PowerShell pipes typically use OEM CP866.
	if out, err := charmap.CodePage866.NewDecoder().Bytes(b); err == nil && utf8.Valid(out) {
		return string(out)
	}
	if out, err := charmap.Windows1251.NewDecoder().Bytes(b); err == nil {
		return string(out)
	}
	return string(b)
}
