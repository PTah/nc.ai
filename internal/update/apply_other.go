// Created by nc.ai 0.8.8.5 on new-admin-pc 2026-10-19 11:39:50
//go:build !windows

package update

// ApplyUpdate: режим самоприменения обновления реализован только для Windows.
// На macOS подмена идёт через shell-скрипт (spawnSwap в install_darwin.go), на
// прочих платформах автообновление не поддержано.
func ApplyUpdate(_ []string) bool {
	return false
}
