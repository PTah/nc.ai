package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (r *Registry) readLints(paths []string) (string, error) {
	root, err := r.WS.ActiveRoot()
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return runGoVet(root, "./...")
	}
	var parts []string
	seenPkg := map[string]bool{}
	for _, rel := range paths {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		full, err := r.WS.Resolve(rel)
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
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
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
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil && out == "" {
		return "", err
	}
	if out == "" {
		return "go vet: clean", nil
	}
	return "go vet:\n" + out, nil
}
