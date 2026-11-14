// Created by nc.ai 1.2.13 on Papasha 2026-11-14 21:55:48
// Package mcp: command resolution so MCP stdio servers run directly instead of
// through a cmd.exe/.cmd shim or an npx wrapper.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// rewriteCommand rewrites cfg.Command/Args so the MCP child runs directly (node
// or python + entry script) instead of through a cmd.exe/.cmd shim or an npx
// wrapper. On Windows those shims spawn `cmd.exe /c ...` plus an extra npx node
// process; the direct form leaves a single node/python process that the tree
// guard can kill cleanly. It is safe to call on an already-resolved config:
// node/python commands pass through unchanged.
func rewriteCommand(cfg *ServerConfig) {
	if cfg == nil || cfg.Transport == "http" {
		return
	}
	cmd := strings.ToLower(strings.TrimSpace(cfg.Command))
	base := strings.ToLower(filepath.Base(strings.TrimSpace(cfg.Command)))

	// npx / npx.cmd / npx-cli.js wrapper → node + npx-cli.js (no cmd.exe, no
	// network: the package is already cached by install-time resolution).
	if cmd == "npx" || base == "npx.cmd" || strings.HasSuffix(base, "npx-cli.js") {
		if pkg := npxPackage(cfg.Args); pkg != "" {
			if node, err := lookPath("node"); err == nil {
				if cli := npxCliJS(); cli != "" {
					cfg.Command = node
					cfg.Args = append([]string{cli, "--yes", pkg}, extraNpxArgs(cfg.Args)...)
					return
				}
			}
		}
	}

	// npm .cmd/.bat shim → node (or python) + entry script.
	if runtime.GOOS == "windows" {
		shim := strings.TrimSpace(cfg.Command)
		if shim == "" {
			return
		}
		if !filepath.IsAbs(shim) {
			if p, err := resolveBinary(shim); err == nil {
				shim = p
			}
		}
		switch strings.ToLower(filepath.Ext(shim)) {
		case ".cmd", ".bat":
			if bin, args, ok := resolveCmdShim(shim); ok {
				cfg.Command = bin
				cfg.Args = append(args, cfg.Args...)
			}
		}
	}
}

// resolveNpxToNode resolves an npx package into a direct `node <entry.js>`
// invocation. With allowNetwork it may run npx once (install-time) to learn the
// package entry; otherwise it only rewrites to `node npx-cli.js` without
// touching the network (runtime path). Returns ok=false when node is absent.
func resolveNpxToNode(ctx context.Context, pkg string, allowNetwork bool) (string, []string, bool) {
	node, err := lookPath("node")
	if err != nil {
		return "", nil, false
	}
	if allowNetwork {
		if entry := resolveNpxPackageEntry(ctx, node, pkg); entry != "" {
			return node, []string{entry}, true
		}
	}
	if cli := npxCliJS(); cli != "" {
		return node, []string{cli, "--yes", pkg}, true
	}
	return "", nil, false
}

// resolveNpxPackageEntry runs npx once to install the package into the npx cache
// and prints the path of its package.json; the entry script is then derived from
// the package `bin` field. Empty on any failure (offline, exports, no bin).
func resolveNpxPackageEntry(ctx context.Context, node, pkg string) string {
	cli := npxCliJS()
	if cli == "" {
		return ""
	}
	script := `console.log(require.resolve(process.argv[1]))`
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := runCmdOut(cctx, node, []string{
		cli, "--yes", "--package", pkg,
		"node", "-e", script, pkg + "/package.json",
	}, nil)
	if err != nil {
		return ""
	}
	pkgJSON := strings.TrimSpace(out)
	if !fileExists(pkgJSON) {
		return ""
	}
	data, err := os.ReadFile(pkgJSON)
	if err != nil {
		return ""
	}
	var meta struct {
		Bin any `json:"bin"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	entry := binEntry(meta.Bin)
	if entry == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(pkgJSON), filepath.FromSlash(entry))
}

// binEntry extracts the first entry path from a package.json `bin` value, which
// may be a string ("bin": "./cli.js") or a map of names to paths.
func binEntry(bin any) string {
	switch v := bin.(type) {
	case string:
		return v
	case map[string]any:
		for _, vv := range v {
			if s, ok := vv.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// npxPackage returns the package name from npx args, e.g. ["-y", "pkg"] → "pkg".
func npxPackage(args []string) string {
	for i, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if a == "-y" || a == "--yes" || strings.HasPrefix(a, "--package") || a == "-p" {
			continue
		}
		if strings.HasPrefix(a, "--package=") {
			return strings.TrimPrefix(a, "--package=")
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		// first non-flag token after the package switches is the package
		_ = i
		return a
	}
	return ""
}

// extraNpxArgs returns server arguments that follow the package name (everything
// after the first non-flag token), preserving flags the server itself needs.
func extraNpxArgs(args []string) []string {
	seenPkg := false
	var out []string
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !seenPkg {
			if a == "-y" || a == "--yes" || a == "-p" || strings.HasPrefix(a, "--package") {
				continue
			}
			if strings.HasPrefix(a, "-") {
				continue
			}
			seenPkg = true
			continue
		}
		out = append(out, a)
	}
	return out
}

var (
	npxCliRe     = regexp.MustCompile(`(?i)["']?((?:%~dp0|%dp0%)\\[^"'\r\n]*npx-cli\.js)["']?`)
	shimScriptRe = regexp.MustCompile(`(?i)(?:%~dp0|%dp0%)\\([^"%\r\n]+?\.(?:js|mjs|cjs))`)
)

// npxCliJS finds the real npx-cli.js entry next to the npm/npx shim, so we can
// run it with node directly and avoid the cmd.exe wrapper on Windows.
func npxCliJS() string {
	if p, err := resolveBinary("npx"); err == nil {
		if cli := npxCliFromShim(p); cli != "" {
			return cli
		}
	}
	if p, err := resolveBinary("npm"); err == nil {
		dir := filepath.Dir(p)
		for _, rel := range []string{
			"npx-cli.js",
			filepath.Join("node_modules", "npm", "bin", "npx-cli.js"),
		} {
			cand := filepath.Join(dir, rel)
			if fileExists(cand) {
				return cand
			}
		}
	}
	return ""
}

func npxCliFromShim(shim string) string {
	data, err := os.ReadFile(shim)
	if err != nil {
		return ""
	}
	m := npxCliRe.FindSubmatch(data)
	if len(m) < 2 {
		return ""
	}
	rel := string(m[1])
	rel = strings.ReplaceAll(rel, "%~dp0\\", "")
	rel = strings.ReplaceAll(rel, "%dp0%\\", "")
	abs := filepath.Join(filepath.Dir(shim), filepath.FromSlash(rel))
	if fileExists(abs) {
		return abs
	}
	return ""
}

// resolveCmdShim parses a generated npm .cmd shim and returns the interpreter
// (node or python) and the absolute entry script, so the process can be started
// without cmd.exe. Returns ok=false when the shim cannot be understood.
func resolveCmdShim(shim string) (string, []string, bool) {
	data, err := os.ReadFile(shim)
	if err != nil {
		return "", nil, false
	}
	loc := shimScriptRe.FindSubmatchIndex(data)
	if loc == nil || len(loc) < 4 {
		return "", nil, false
	}
	rel := string(data[loc[2]:loc[3]])
	abs := filepath.Join(filepath.Dir(shim), filepath.FromSlash(rel))
	if !fileExists(abs) {
		return "", nil, false
	}
	bin := "node"
	if bytes.Contains(bytes.ToLower(data), []byte("python")) {
		bin = "python"
	}
	path, err := resolveBinary(bin)
	if err != nil {
		return "", nil, false
	}
	return path, []string{abs}, true
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
