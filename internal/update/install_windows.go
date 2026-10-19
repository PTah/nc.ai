// Edited by nc.ai 0.8.8.5 on new-admin-pc 2026-10-19 11:39:50
//go:build windows

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// spawnSwap запускает «самоприменение» обновления тем же бинарём с флагом
// --apply-update: он дождётся выхода приложения, подменит exe и перезапустит его.
//
// Никакого PowerShell / -ExecutionPolicy Bypass / временных .ps1 — именно на эту
// связку («скрытый дроппер») реагирует эвристика антивируса
// (Trojan:Win32/Bearfoos.A!ml). Теперь подмену делает сам NotCursor.exe.
func spawnSwap(stage, unpacked, appExe string) error {
	return spawnSwapFor(stage, unpacked, appExe, os.Getpid())
}

const (
	// createNoWindow — «тихая» консоль без окна (для GUI-билда не критично).
	createNoWindow = 0x08000000
	// createNewProcessGroup — свой процесс-группа, чтобы Ctrl+C в консоли
	// приложения не задел подменщика.
	createNewProcessGroup = 0x00000200
	// createBreakawayFromJob — если приложение в Job (WebView2/Wails), без
	// этого флага Windows убивает helper вместе с родительским процессом.
	createBreakawayFromJob = 0x01000000
	// processQueryLimitedInformation — доступ к коду выхода чужого процесса.
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

func processAlive(pid int) bool {
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

func spawnSwapFor(stage, unpacked, appExe string, targetPid int) error {
	newExe, ok := findWindowsExe(unpacked)
	if !ok {
		return errors.New("в архиве обновления не найден NotCursor.exe")
	}
	logPath := filepath.Join(stage, "apply.log")
	cmd := exec.Command(newExe,
		applyUpdateFlag,
		"--wait-pid", fmt.Sprint(targetPid),
		"--new", newExe,
		"--target", appExe,
		"--log", logPath,
	)
	cmd.Dir = stage
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow | createNewProcessGroup | createBreakawayFromJob,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить подмену: %w", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()

	// Подменщик должен дойти до первой строки лога. Если он умер сразу — не
	// закрываем приложение впустую, а сообщаем, что подмена не запустилась.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fi, statErr := os.Stat(logPath); statErr == nil && fi.Size() > 0 {
			return nil
		}
		if !processAlive(pid) {
			return errors.New("подменщик не запустился")
		}
		time.Sleep(150 * time.Millisecond)
	}
	// Живой, просто ещё не записал лог — не мешаем.
	return nil
}
