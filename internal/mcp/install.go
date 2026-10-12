package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// InstallResult is returned after EnsureInstalled.
type InstallResult struct {
	PresetID string `json:"presetId"`
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
	Command  string `json:"command,omitempty"`
	Args     []string `json:"args,omitempty"`
	Already  bool   `json:"already,omitempty"`
}

// EnsureInstalled downloads the preset package if needed and returns a ServerConfig
// ready to save (absolute binary path when resolved).
func EnsureInstalled(ctx context.Context, presetID string) (ServerConfig, InstallResult, error) {
	p, ok := PresetByID(presetID)
	if !ok {
		return ServerConfig{}, InstallResult{}, fmt.Errorf("unknown MCP preset %q", presetID)
	}
	cfg := p.Server
	cfg.Normalize()
	res := InstallResult{PresetID: p.ID}

	switch strings.ToLower(p.Install.Kind) {
	case "npx":
		if _, err := exec.LookPath("npx"); err != nil {
			return cfg, res, fmt.Errorf("npx не найден — установите Node.js")
		}
		cfg.Command = "npx"
		cfg.Args = []string{"-y", p.Install.Package}
		res.OK = true
		res.Already = true
		res.Message = "npx подтянет пакет при первом запуске: " + p.Install.Package
		res.Command, res.Args = cfg.Command, cfg.Args
		return cfg, res, nil

	case "uvx":
		uvx, err := lookPath("uvx")
		if err != nil {
			return cfg, res, fmt.Errorf("uvx не найден — установите uv (https://docs.astral.sh/uv/)")
		}
		// Prefetch into uv cache (best-effort).
		preCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		pkg := p.Install.Package
		if !strings.Contains(pkg, "@") {
			pkg = pkg + "@latest"
		}
		_ = runCmd(preCtx, uvx, []string{pkg, "--help"}, nil)
		cfg.Command = uvx
		cfg.Args = []string{pkg}
		res.OK = true
		res.Message = "uvx: пакет " + pkg + " (кэш обновлён при возможности)"
		res.Command, res.Args = cfg.Command, cfg.Args
		return cfg, res, nil

	case "pip":
		bin := p.Install.Binary
		if path, err := resolveBinary(bin); err == nil {
			cfg.Command = path
			cfg.Args = append([]string(nil), p.Install.ServeArgs...)
			res.OK = true
			res.Already = true
			res.Message = "уже установлено: " + path
			res.Command, res.Args = cfg.Command, cfg.Args
			return cfg, res, nil
		}
		py, err := lookPath("python")
		if err != nil {
			py, err = lookPath("python3")
		}
		if err != nil {
			return cfg, res, fmt.Errorf("python не найден — нужен Python 3.10+")
		}
		instCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		out, err := runCmdOut(instCtx, py, []string{"-m", "pip", "install", "--user", p.Install.Package}, nil)
		if err != nil {
			return cfg, res, fmt.Errorf("pip install %s: %w\n%s", p.Install.Package, err, truncateOut(out, 800))
		}
		path, err := resolveBinary(bin)
		if err != nil {
			// Fallback: python -m may not expose Scripts on PATH — try known dirs.
			return cfg, res, fmt.Errorf("пакет установлен, но бинарь %q не найден в PATH/Scripts: добавьте Scripts в PATH", bin)
		}
		cfg.Command = path
		cfg.Args = append([]string(nil), p.Install.ServeArgs...)
		res.OK = true
		res.Message = "установлено через pip --user: " + path
		res.Command, res.Args = cfg.Command, cfg.Args
		return cfg, res, nil

	case "npm":
		bin := p.Install.Binary
		if path, err := resolveBinary(bin); err == nil {
			cfg.Command = path
			cfg.Args = append([]string(nil), p.Install.ServeArgs...)
			res.OK = true
			res.Already = true
			res.Message = "уже установлено: " + path
			res.Command, res.Args = cfg.Command, cfg.Args
			return cfg, res, nil
		}
		npm, err := lookPath("npm")
		if err != nil {
			return cfg, res, fmt.Errorf("npm не найден — установите Node.js")
		}
		instCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		out, err := runCmdOut(instCtx, npm, []string{"install", "-g", p.Install.Package}, nil)
		if err != nil {
			return cfg, res, fmt.Errorf("npm install -g %s: %w\n%s", p.Install.Package, err, truncateOut(out, 800))
		}
		path, err := resolveBinary(bin)
		if err != nil {
			// npx fallback so the preset still works.
			cfg.Command = "npx"
			cfg.Args = []string{"-y", p.Install.Package}
			res.OK = true
			res.Message = "npm install прошёл, бинарь не в PATH — используем npx -y " + p.Install.Package
			res.Command, res.Args = cfg.Command, cfg.Args
			return cfg, res, nil
		}
		cfg.Command = path
		cfg.Args = append([]string(nil), p.Install.ServeArgs...)
		res.OK = true
		res.Message = "установлено через npm -g: " + path
		res.Command, res.Args = cfg.Command, cfg.Args
		return cfg, res, nil

	default:
		return cfg, res, fmt.Errorf("unknown install kind %q", p.Install.Kind)
	}
}

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

func resolveBinary(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("empty binary")
	}
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath(name + ".exe"); err == nil {
			return path, nil
		}
		if path, err := exec.LookPath(name + ".cmd"); err == nil {
			return path, nil
		}
	}
	for _, dir := range candidateBinDirs() {
		for _, cand := range []string{name, name + ".exe", name + ".cmd"} {
			p := filepath.Join(dir, cand)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s not found", name)
}

func candidateBinDirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	if runtime.GOOS == "windows" {
		dirs = append(dirs,
			filepath.Join(home, "AppData", "Roaming", "npm"),
			filepath.Join(home, "AppData", "Roaming", "Python", "Python312", "Scripts"),
			filepath.Join(home, "AppData", "Roaming", "Python", "Python313", "Scripts"),
			filepath.Join(home, "AppData", "Local", "Programs", "Python", "Python312", "Scripts"),
			filepath.Join(home, "AppData", "Local", "Programs", "Python", "Python313", "Scripts"),
			filepath.Join(home, ".local", "bin"),
		)
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Programs", "Python", "Python312", "Scripts"))
		}
		if app := os.Getenv("APPDATA"); app != "" {
			dirs = append(dirs, filepath.Join(app, "npm"), filepath.Join(app, "Python", "Python312", "Scripts"))
		}
	} else {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			"/usr/local/bin",
			filepath.Join(home, ".npm-global", "bin"),
		)
	}
	return dirs
}

func runCmd(ctx context.Context, bin string, args []string, env []string) error {
	_, err := runCmdOut(ctx, bin, args, env)
	return err
}

func runCmdOut(ctx context.Context, bin string, args []string, env []string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	if len(env) > 0 {
		cmd.Env = env
	} else {
		cmd.Env = os.Environ()
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func truncateOut(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
