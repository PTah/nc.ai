//go:build windows

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// swapScript ждёт выхода приложения, подменяет exe и запускает новую версию.
// Файл занят, пока процесс жив, поэтому порядок именно такой: ждём/снимаем
// процесс, только потом переименовываем старый exe и копируем новый. Если
// копирование не удалось (например, каталог только для чтения), возвращаем
// на место старый exe и всё равно запускаем приложение.
const swapScript = `param(
  [Parameter(Mandatory=$true)][int]$TargetPid,
  [Parameter(Mandatory=$true)][string]$NewExe,
  [Parameter(Mandatory=$true)][string]$AppExe,
  [string]$Log
)
$ErrorActionPreference = 'SilentlyContinue'
function Note([string]$m) { if ($Log) { Add-Content -LiteralPath $Log -Value ("{0} {1}" -f (Get-Date -Format o), $m) } }
Note "waiting for pid $TargetPid"
try { Wait-Process -Id $TargetPid -Timeout 30 -ErrorAction Stop } catch { Note "wait timed out" }
if (Get-Process -Id $TargetPid -ErrorAction SilentlyContinue) {
  Note "stopping pid $TargetPid"
  Stop-Process -Id $TargetPid -Force -ErrorAction SilentlyContinue
}
Start-Sleep -Milliseconds 800
$old = "$AppExe.old"
Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
if (Test-Path -LiteralPath $AppExe) {
  Move-Item -LiteralPath $AppExe -Destination $old -Force -ErrorAction SilentlyContinue
}
$replaced = $false
try {
  Copy-Item -LiteralPath $NewExe -Destination $AppExe -Force -ErrorAction Stop
  $replaced = $true
  Note "exe replaced"
} catch {
  Note ("copy failed: " + $_.Exception.Message)
}
if (-not $replaced -and (Test-Path -LiteralPath $old)) {
  Move-Item -LiteralPath $old -Destination $AppExe -Force -ErrorAction SilentlyContinue
}
Start-Process -FilePath $AppExe
Start-Sleep -Seconds 2
if ($replaced) { Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue }
`

// spawnSwap пишет скрипт подмены и запускает его отдельным процессом, который
// переживёт выход приложения.
func spawnSwap(stage, unpacked, appExe string) error {
	return spawnSwapFor(stage, unpacked, appExe, os.Getpid())
}

const (
	// createNoWindow — «тихая» консоль без окна.
	//
	// DETACHED_PROCESS (0x8) здесь использовать нельзя: PowerShell 5.1 с этим
	// флагом стартует и умирает, не выполнив ни одной строки скрипта — ни
	// swap.log, ни подмены exe (проверено тестом TestSwapLaunchesHelperProcess).
	createNoWindow = 0x08000000
	// createNewProcessGroup — свой процесс-группа, чтобы Ctrl+C в консоли
	// приложения не задел подменщика.
	createNewProcessGroup = 0x00000200
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
	script := filepath.Join(stage, "swap.ps1")
	// UTF-8 с BOM: PowerShell 5.1 иначе читает .ps1 как ANSI (правило проекта).
	if err := os.WriteFile(script, append([]byte{0xEF, 0xBB, 0xBF}, swapScript...), 0o644); err != nil {
		return err
	}
	logPath := filepath.Join(stage, "swap.log")
	errPath := filepath.Join(stage, "swap.err")
	errFile, err := os.Create(errPath)
	if err != nil {
		return err
	}
	defer errFile.Close()

	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", script,
		"-TargetPid", fmt.Sprint(targetPid),
		"-NewExe", newExe,
		"-AppExe", appExe,
		"-Log", logPath,
	)
	cmd.Dir = stage
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, errFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow | createNewProcessGroup,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить подмену: %w", err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()

	// Подменщик должен дойти до первой строки (она пишет swap.log). Если он умер
	// сразу — сообщаем причину из swap.err, а не оставляем «скачал, но ничего не
	// произошло»: в этом случае приложение не будет закрыто.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fi, statErr := os.Stat(logPath); statErr == nil && fi.Size() > 0 {
			return nil
		}
		if !processAlive(pid) {
			msg, _ := os.ReadFile(errPath)
			if len(msg) == 0 {
				msg = []byte("подменщик не оставил сообщений")
			}
			return fmt.Errorf("подмена не запустилась: %s", strings.TrimSpace(string(msg)))
		}
		time.Sleep(150 * time.Millisecond)
	}
	// Процесс жив, просто ещё не дошёл до записи в лог — не мешаем ему.
	return nil
}
