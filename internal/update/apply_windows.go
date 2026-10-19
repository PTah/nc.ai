// Created by nc.ai 0.8.8.5 on new-admin-pc 2026-10-19 11:39:50
//go:build windows

package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// applyUpdateFlag — режим самоприменения обновления (запускается spawnSwap).
const applyUpdateFlag = "--apply-update"

// ApplyUpdate обрабатывает режим --apply-update: подменяет exe и перезапускает
// приложение. Возвращает false для обычного запуска (флаг не передан) — тогда
// main() продолжает обычную инициализацию UI.
func ApplyUpdate(args []string) bool {
	var (
		apply   bool
		waitPid int
		newExe  string
		target  string
		logPath string
	)
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case applyUpdateFlag:
			apply = true
		case "--wait-pid":
			if i+1 < len(args) {
				i++
				waitPid, _ = strconv.Atoi(args[i])
			}
		case "--new":
			if i+1 < len(args) {
				i++
				newExe = args[i]
			}
		case "--target":
			if i+1 < len(args) {
				i++
				target = args[i]
			}
		case "--log":
			if i+1 < len(args) {
				i++
				logPath = args[i]
			}
		}
	}
	if !apply {
		return false
	}
	os.Exit(applyUpdate(waitPid, newExe, target, logPath))
	return true
}

func applyNote(logPath, msg string) {
	if logPath == "" {
		return
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
	_ = f.Close()
}

// applyUpdate — сама подмена: ждём выхода старого процесса, переименовываем
// старый exe в .old, копируем новый, перезапускаем, убираем .old. Возвращает
// код выхода helper-процесса (0 — успех).
func applyUpdate(waitPid int, newExe, target, logPath string) int {
	applyNote(logPath, fmt.Sprintf("apply start waitPid=%d new=%s target=%s", waitPid, newExe, target))
	if newExe == "" || target == "" {
		applyNote(logPath, "missing --new/--target")
		return 2
	}
	// Ждём, пока старое приложение завершится (иначе exe занят).
	deadline := time.Now().Add(45 * time.Second)
	for waitPid > 0 && time.Now().Before(deadline) {
		if !processAlive(waitPid) {
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if waitPid > 0 && processAlive(waitPid) {
		applyNote(logPath, "old process still alive, terminating")
		if h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(waitPid)); err == nil {
			_ = syscall.TerminateProcess(h, 1)
			_ = syscall.CloseHandle(h)
		}
	}
	time.Sleep(800 * time.Millisecond)

	old := target + ".old"
	_ = os.Remove(old)
	if err := os.Rename(target, old); err != nil {
		applyNote(logPath, "move to .old failed: "+err.Error())
	}
	replaced := false
	if err := copyFile(newExe, target); err != nil {
		applyNote(logPath, "copy new exe failed: "+err.Error())
	} else {
		replaced = true
		applyNote(logPath, "exe replaced")
	}
	if !replaced {
		if _, err := os.Stat(old); err == nil {
			_ = os.Rename(old, target)
			applyNote(logPath, "restored previous exe")
		}
	} else {
		_ = os.Remove(old)
	}
	if _, err := os.Stat(target); err != nil {
		applyNote(logPath, "FATAL: target missing after swap")
		return 1
	}
	// Перезапуск приложения (до трёх попыток).
	dir := filepath.Dir(target)
	started := false
	for i := 0; i < 3 && !started; i++ {
		cmd := exec.Command(target)
		cmd.Dir = dir
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: createNoWindow | createNewProcessGroup | createBreakawayFromJob,
		}
		if err := cmd.Start(); err == nil {
			_ = cmd.Process.Release()
			started = true
			applyNote(logPath, fmt.Sprintf("relaunched attempt=%d", i+1))
		} else {
			applyNote(logPath, "relaunch failed: "+err.Error())
		}
		time.Sleep(time.Second)
	}
	if !started {
		applyNote(logPath, "FATAL: could not relaunch application")
	}
	time.Sleep(2 * time.Second)
	return 0
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
