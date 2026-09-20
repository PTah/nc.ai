package update

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Install скачивает файл обновления, распаковывает его во временный каталог и
// запускает отдельный процесс, который после выхода приложения подменит файлы
// и запустит новую версию. Успешный возврат означает, что приложению можно
// завершаться (иначе подмена не удастся: файлы заняты).
func Install(ctx context.Context, info Info, progress func(done, total int64)) (string, error) {
	if !info.Available {
		return "", errors.New("обновление не требуется")
	}
	if !info.AssetFound || info.AssetURL == "" {
		return "", errors.New("в релизе нет файла для этой платформы — скачайте вручную")
	}
	stage, err := os.MkdirTemp("", "nc-update-"+safeVersion(info.Latest)+"-")
	if err != nil {
		return "", err
	}
	zipPath := filepath.Join(stage, filepath.Base(info.AssetName))
	if err := Download(ctx, info.AssetURL, zipPath, progress); err != nil {
		return stage, err
	}
	unpacked := filepath.Join(stage, "new")
	if err := unzip(zipPath, unpacked); err != nil {
		return stage, err
	}
	exe, err := os.Executable()
	if err != nil {
		return stage, err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil && resolved != "" {
		exe = resolved
	}
	if err := spawnSwap(stage, unpacked, exe); err != nil {
		return stage, err
	}
	return stage, nil
}

// safeVersion оставляет в имени каталога только то, что безопасно для файловой
// системы: буквы/цифры, одиночные '.', одиночные '-'; никаких «..» и разделителей.
func safeVersion(v string) string {
	v = strings.TrimSpace(v)
	var b strings.Builder
	lastDot := false
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastDot = false
		case r == '.':
			if lastDot {
				continue // не даём собрать «..»
			}
			b.WriteRune(r)
			lastDot = true
		case r == '-':
			b.WriteRune(r)
			lastDot = false
		}
	}
	out := strings.Trim(b.String(), ".-")
	if out == "" {
		return "next"
	}
	return out
}

// unzip распаковывает архив во каталог destDir, отбрасывая небезопасные пути.
func unzip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("архив повреждён: %w", err)
	}
	defer r.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	root := filepath.Clean(destDir) + string(os.PathSeparator)
	for _, f := range r.File {
		name := filepath.Clean(filepath.FromSlash(f.Name))
		if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			continue // защита от «../../etc/passwd»
		}
		target := filepath.Join(destDir, name)
		if !strings.HasPrefix(target, root) {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode().Perm()|0o600)
		if err != nil {
			src.Close()
			return err
		}
		if _, err := io.Copy(dst, io.LimitReader(src, maxAssetBytes)); err != nil {
			src.Close()
			dst.Close()
			return err
		}
		src.Close()
		if err := dst.Close(); err != nil {
			return err
		}
	}
	return nil
}

// findEntry ищет в распакованном каталоге файл/каталог по предикату
// (не глубже трёх уровней — релизные архивы плоские).
func findEntry(dir string, match func(name string, isDir bool) bool) (string, bool) {
	var found string
	var walk func(path string, level int)
	walk = func(path string, level int) {
		if found != "" || level > 3 {
			return
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := filepath.Join(path, e.Name())
			if match(e.Name(), e.IsDir()) {
				found = p
				return
			}
			if e.IsDir() {
				walk(p, level+1)
			}
		}
	}
	walk(dir, 0)
	return found, found != ""
}

// findWindowsExe находит новый NotCursor*.exe во распакованном архиве.
func findWindowsExe(dir string) (string, bool) {
	if p, ok := findEntry(dir, func(name string, isDir bool) bool {
		return !isDir && strings.EqualFold(name, "NotCursor.exe")
	}); ok {
		return p, true
	}
	return findEntry(dir, func(name string, isDir bool) bool {
		return !isDir && strings.HasSuffix(strings.ToLower(name), ".exe")
	})
}

// findMacApp находит новый NotCursor.app во распакованном архиве.
func findMacApp(dir string) (string, bool) {
	return findEntry(dir, func(name string, isDir bool) bool {
		return isDir && strings.HasSuffix(strings.ToLower(name), ".app")
	})
}
