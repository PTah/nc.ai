// Created by nc.ai 1.2.13 on Papasha 2026-11-14 22:20:00
package mcp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// npxShimBody is the shape of the real npm-generated npx shim (both npx.cmd on
// Windows and npx in the npm global bin on Unix): the launcher path is written
// relative to the shim directory via %dp0% / %~dp0.
const npxShimBody = "@ECHO off\r\n" +
	"SETLOCAL\r\n" +
	"CALL :find_dp0\r\n" +
	"IF EXIST \"%dp0%\\node.exe\" (\r\n" +
	"  SET \"_prog=%dp0%\\node.exe\"\r\n" +
	") ELSE (\r\n" +
	"  SET \"_prog=node\"\r\n" +
	")\r\n" +
	"endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & \"%_prog%\"  \"%dp0%\\node_modules\\npm\\bin\\npx-cli.js\" %*\r\n"

func TestBinEntry(t *testing.T) {
	if got := binEntry("./cli.js"); got != "./cli.js" {
		t.Fatalf("string bin=%q", got)
	}
	if got := binEntry(map[string]any{"name": "./bin/server.js"}); got != "./bin/server.js" {
		t.Fatalf("map bin=%q", got)
	}
	if got := binEntry(nil); got != "" {
		t.Fatalf("nil bin=%q", got)
	}
}

func TestNpxPackageAndExtraArgs(t *testing.T) {
	args := []string{"--yes", "--package", "@scope/pkg", "--verbose"}
	if got := npxPackage([]string{"-y", "@codebase-memory/mcp"}); got != "@codebase-memory/mcp" {
		t.Fatalf("npxPackage=%q", got)
	}
	// extraNpxArgs keeps everything that follows the package name.
	if got := extraNpxArgs([]string{"-y", "some-pkg", "--root", "/tmp"}); strings.Join(got, " ") != "--root /tmp" {
		t.Fatalf("extraNpxArgs=%v", got)
	}
	// `--package <pkg>` form: the package switch itself and its value are not
	// server arguments.
	if got := npxPackage(args); got != "@scope/pkg" {
		t.Fatalf("--package form resolved to %q", got)
	}
	if got := extraNpxArgs(args); len(got) != 1 || got[0] != "--verbose" {
		t.Fatalf("extraNpxArgs(--package form)=%v", got)
	}
}

// npxCliFromShim must resolve the launcher next to a shim without running it —
// that is what keeps node.exe from being started twice at runtime.
func TestNpxCliFromShim(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "node_modules", "npm", "bin", "npx-cli.js")
	if err := os.MkdirAll(filepath.Dir(cli), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("// npx entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, shimName("npx"))
	if err := os.WriteFile(shim, []byte(npxShimBody), 0o755); err != nil {
		t.Fatal(err)
	}
	got := npxCliFromShim(shim)
	if !samePath(got, cli) {
		t.Fatalf("npxCliFromShim=%q want %q", got, cli)
	}

	// npxCliJS finds the shim through PATH and resolves the same launcher.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if got2 := npxCliJS(); !samePath(got2, cli) {
		t.Fatalf("npxCliJS=%q want %q", got2, cli)
	}
}

// rewriteCommand must not touch an already-direct command (idempotent on a
// config that EnsureInstalled already resolved).
func TestRewriteCommandKeepsDirectNode(t *testing.T) {
	cfg := ServerConfig{ID: "x", Transport: "stdio", Command: "node", Args: []string{"server.js", "--root", "/tmp"}}
	rewriteCommand(&cfg)
	if cfg.Command != "node" || strings.Join(cfg.Args, " ") != "server.js --root /tmp" {
		t.Fatalf("direct node config was rewritten: %q %v", cfg.Command, cfg.Args)
	}

	nilCfg := (*ServerConfig)(nil)
	rewriteCommand(nilCfg) // must not panic

	httpCfg := ServerConfig{Transport: "http", Command: "npx", Args: []string{"-y", "pkg"}}
	rewriteCommand(&httpCfg)
	if httpCfg.Command != "npx" {
		t.Fatalf("http config must be left alone, got %q", httpCfg.Command)
	}
}

// An npx command becomes `node <npx-cli.js> --yes <pkg>` so no cmd.exe wrapper
// (and no second node process) is spawned at runtime.
func TestRewriteCommandNpxToNode(t *testing.T) {
	if _, err := lookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	dir := t.TempDir()
	cli := filepath.Join(dir, "node_modules", "npm", "bin", "npx-cli.js")
	if err := os.MkdirAll(filepath.Dir(cli), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cli, []byte("// npx entry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, shimName("npx"))
	if err := os.WriteFile(shim, []byte(npxShimBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	cfg := ServerConfig{ID: "x", Transport: "stdio", Command: "npx", Args: []string{"-y", "@scope/pkg", "--root", "/tmp"}}
	rewriteCommand(&cfg)
	node, _ := lookPath("node")
	if !samePath(cfg.Command, node) {
		t.Fatalf("command=%q want node %q", cfg.Command, node)
	}
	if len(cfg.Args) != 5 || !samePath(cfg.Args[0], cli) || cfg.Args[1] != "--yes" ||
		cfg.Args[2] != "@scope/pkg" || cfg.Args[3] != "--root" || cfg.Args[4] != "/tmp" {
		t.Fatalf("args=%v (want [npx-cli.js --yes @scope/pkg --root /tmp])", cfg.Args)
	}
}

// resolveCmdShim turns an npm .cmd launcher into `node <entry.js>`.
func TestResolveCmdShim(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "bin", "server.js")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("// server\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, "pkg-mcp.cmd")
	body := "@ECHO off\r\n\"%dp0%\\bin\\server.js\" %*\r\n"
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	bin, args, ok := resolveCmdShim(shim)
	if _, err := lookPath("node"); err != nil {
		if ok {
			t.Fatal("resolveCmdShim invented an interpreter without node installed")
		}
	} else {
		interp := strings.TrimSuffix(strings.ToLower(filepath.Base(bin)), ".exe")
		if !ok || len(args) != 1 || !samePath(args[0], entry) || interp != "node" {
			t.Fatalf("resolveCmdShim bin=%q args=%v ok=%v", bin, args, ok)
		}
	}

	// A shim we cannot understand must be reported as such, not guessed.
	junk := filepath.Join(dir, "junk.cmd")
	if err := os.WriteFile(junk, []byte("@ECHO off\r\necho hello\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := resolveCmdShim(junk); ok {
		t.Fatal("unparsable shim reported as resolved")
	}
	if _, _, ok := resolveCmdShim(filepath.Join(dir, "missing.cmd")); ok {
		t.Fatal("missing shim reported as resolved")
	}
}

// shimName is the file name npm uses for a launcher on this OS.
func shimName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".cmd"
	}
	return base
}

// samePath compares two paths tolerantly (case, separators, symlinks).
func samePath(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" {
		return a == b
	}
	if abs, err := filepath.Abs(a); err == nil {
		a = abs
	}
	if abs, err := filepath.Abs(b); err == nil {
		b = abs
	}
	if real, err := filepath.EvalSymlinks(a); err == nil {
		a = real
	}
	if real, err := filepath.EvalSymlinks(b); err == nil {
		b = real
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
