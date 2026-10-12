package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// mock server: go run testdata/mock_server.go
func TestStdioInitializeAndCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	mock := filepath.Join("testdata", "mock_server.go")
	cfg := ServerConfig{
		ID:         "mock",
		Label:      "Mock",
		Enabled:    true,
		Transport:  "stdio",
		Command:    goBin(),
		Args:       []string{"run", mock},
		TimeoutSec: 10,
		EnabledTools: []string{"echo"},
	}
	sess, err := StartStdio(ctx, cfg)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer sess.Close()

	tools := sess.Tools()
	if len(tools) == 0 || tools[0].Name != "echo" {
		t.Fatalf("tools=%v", tools)
	}
	out, err := sess.CallTool(ctx, "echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out != "hi" {
		t.Fatalf("out=%q", out)
	}
}

func TestManagerToolSpecsAndCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	mock := filepath.Join("testdata", "mock_server.go")
	m := NewManager()
	m.SetConfigs([]ServerConfig{{
		ID:           "mock",
		Label:        "Mock",
		Enabled:      true,
		Transport:    "stdio",
		Command:      goBin(),
		Args:         []string{"run", mock},
		TimeoutSec:   10,
		EnabledTools: []string{"echo"},
	}})
	if _, err := m.Start(ctx, "mock"); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer m.CloseAll()

	specs := m.ToolSpecs()
	if len(specs) != 1 || specs[0].Function.Name != "mcp:mock:echo" {
		t.Fatalf("specs=%v", specs)
	}
	out, err := m.Call(ctx, "mcp:mock:echo", `{"text":"ping"}`)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if out != "ping" {
		t.Fatalf("out=%q", out)
	}

	// disabled tool
	m.SetConfigs([]ServerConfig{{
		ID: "mock", Enabled: true, Transport: "stdio",
		Command: goBin(), Args: []string{"run", mock},
		EnabledTools: nil,
	}})
	// session still live but allowlist empty
	specs = m.ToolSpecs()
	if len(specs) != 0 {
		t.Fatalf("want empty specs when allowlist empty, got %d", len(specs))
	}
}

func TestSplitPrefixed(t *testing.T) {
	s, tool, ok := SplitPrefixed("mcp:codegraph:find_symbol")
	if !ok || s != "codegraph" || tool != "find_symbol" {
		t.Fatalf("got %s %s %v", s, tool, ok)
	}
	if _, _, ok := SplitPrefixed("read_file"); ok {
		t.Fatal("read_file must not parse as mcp")
	}
}

func TestFilteredEnvDropsAPIKeys(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "secret")
	t.Setenv("PATH", os.Getenv("PATH"))
	env := filteredEnv(map[string]string{"FOO": "bar"})
	joined, _ := json.Marshal(env)
	if strings.Contains(string(joined), "secret") {
		t.Fatal("provider key leaked into MCP env")
	}
	found := false
	for _, kv := range env {
		if kv == "FOO=bar" {
			found = true
		}
	}
	if !found {
		t.Fatal("extra env missing")
	}
}

func goBin() string {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("go.exe"); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return "go"
}
