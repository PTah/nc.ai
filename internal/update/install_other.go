//go:build !windows && !darwin

package update

import "errors"

// spawnSwap: автообновление «на месте» реализовано только для Windows и macOS.
func spawnSwap(_, _, _ string) error {
	return errors.New("автообновление на этой платформе не поддержано — скачайте релиз вручную")
}
