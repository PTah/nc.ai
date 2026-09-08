package fsx

import (
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path via a temp file + rename so a crash
// mid-write never leaves a truncated JSON. A rotating .bak backup of the
// previous good version is kept alongside.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName) // no-op after successful rename
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	// Rotate previous versions: .bak (prev) -> .bak1 -> .bak2 (best-effort).
	if old, err := os.ReadFile(path); err == nil && len(old) > 0 {
		_ = os.Remove(path + ".bak2")
		_ = os.Rename(path+".bak1", path+".bak2")
		_ = os.Rename(path+".bak", path+".bak1")
		_ = os.WriteFile(path+".bak", old, perm)
	}
	return os.Rename(tmpName, path)
}
