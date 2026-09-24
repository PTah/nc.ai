//go:build windows

package shell

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

const createNoWindow = 0x08000000

// ConfigureCmd hides the console window Windows would otherwise flash for the
// child process (CREATE_NO_WINDOW + HideWindow). Needed for every helper process
// the app spawns — git, shells, version probes — so the UI does not blink with
// cmd/PowerShell windows.
func ConfigureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// prepareTree оставлен no-op: на Windows дерево гасим через taskkill (см. killTree).
func prepareTree(cmd *exec.Cmd) {}

// killTree убивает процесс вместе с детьми: cmd.Process.Kill() гасит только
// оболочку, а её потомки (go build, docker, серверы) продолжают держать пайпы —
// из-за этого таймаут «залипает», а в системе остаются висящие процессы.
func killTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	// taskkill ограничиваем по времени: он часть пути гашения и не должен
	// задерживать возврат из команды.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	kill := exec.CommandContext(ctx, "taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	ConfigureCmd(kill)
	_ = kill.Run()
	_ = cmd.Process.Kill()
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
