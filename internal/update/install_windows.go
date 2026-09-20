//go:build windows

package update

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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

// spawnSwap пишет скрипт подмены и запускает его отдельным (detached) процессом,
// который переживёт выход приложения.
func spawnSwap(stage, unpacked, appExe string) error {
	newExe, ok := findWindowsExe(unpacked)
	if !ok {
		return errors.New("в архиве обновления не найден NotCursor.exe")
	}
	script := filepath.Join(stage, "swap.ps1")
	if err := os.WriteFile(script, []byte(swapScript), 0o644); err != nil {
		return err
	}
	logPath := filepath.Join(stage, "swap.log")
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script,
		"-TargetPid", fmt.Sprint(os.Getpid()),
		"-NewExe", newExe,
		"-AppExe", appExe,
		"-Log", logPath,
	)
	cmd.Dir = stage
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	// DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP: процесс не привязан к нашему
	// и не умрёт вместе с приложением.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
