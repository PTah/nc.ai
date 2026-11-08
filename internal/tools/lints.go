package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"notcursor.ai/app/internal/shell"
)

func (r *Registry) readLints(paths []string) (string, error) {
	folders, err := r.folders()
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		if len(folders) == 1 {
			return runGoVet(folders[0].Path, "./...")
		}
		// Несколько папок: go vet только там, где есть Go-модуль.
		var parts []string
		for _, f := range folders {
			if _, serr := os.Stat(filepath.Join(f.Path, "go.mod")); serr != nil {
				continue
			}
			out, verr := runGoVet(f.Path, "./...")
			if verr != nil && out == "" {
				parts = append(parts, fmt.Sprintf("## %s\n%v", f.Name, verr))
				continue
			}
			parts = append(parts, "## "+f.Name+"\n"+out)
		}
		if len(parts) == 0 {
			return "(в папках рабочего пространства нет Go-модулей — передайте paths)", nil
		}
		return strings.Join(parts, "\n\n"), nil
	}
	var parts []string
	seenPkg := map[string]bool{}
	for _, rel := range paths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		full, err := r.ws().Resolve(rel)
		if err != nil {
			parts = append(parts, fmt.Sprintf("%s: %v", rel, err))
			continue
		}
		switch strings.ToLower(filepath.Ext(full)) {
		case ".go":
			pkg := filepath.Dir(full)
			if seenPkg[pkg] {
				continue
			}
			seenPkg[pkg] = true
			root := folders[0].Path
			if f, ok := folderOfPath(folders, full); ok {
				root = f.Path
			}
			out, err := runGoVet(root, pkg)
			if err != nil && out == "" {
				parts = append(parts, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			if strings.TrimSpace(out) == "" {
				parts = append(parts, fmt.Sprintf("%s: no go vet issues", rel))
			} else {
				parts = append(parts, out)
			}
		default:
			parts = append(parts, fmt.Sprintf("%s: no linter wired for this file type (go vet covers .go)", rel))
		}
	}
	if len(parts) == 0 {
		return "(no paths)", nil
	}
	return strings.Join(parts, "\n\n"), nil
}

func runGoVet(root, pkg string) (string, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return "go is not on PATH — cannot run read_lints", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	arg := pkg
	if pkg != "./..." {
		rel, err := filepath.Rel(root, pkg)
		if err == nil {
			arg = "./" + filepath.ToSlash(rel)
		}
	}
	cmd := exec.CommandContext(ctx, "go", "vet", arg)
	cmd.Dir = root
	shell.ConfigureCmd(cmd)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// go vet компилирует пакеты: на холодном кэше сборки это минуты.
		msg := "go vet не уложился в 2 мин (холодный кэш сборки) — повторите read_lints или один раз соберите проект (go build ./...)"
		if out != "" {
			msg += "\n" + out
		}
		return "", errors.New(msg)
	}
	if err != nil && out == "" {
		return "", err
	}
	if out == "" {
		return "go vet: clean", nil
	}
	return "go vet:\n" + out, nil
}
