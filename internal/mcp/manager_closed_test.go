// Created by nc.ai 1.2.15 on Papasha 2026-11-21 11:07:05
package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestManagerNoStartAfterCloseAll: shutdown must not be undone by a background
// EnsureStartedBackground goroutine that was already inside StartStdio. The
// server it booted used to be registered after CloseAll had walked the map, so
// the process stayed alive under a dead app.
func TestManagerNoStartAfterCloseAll(t *testing.T) {
	m := NewManager()
	m.SetConfigs([]ServerConfig{{
		ID: "mock", Label: "Mock", Enabled: true, Transport: "stdio",
		Command: goBin(), Args: []string{"run", filepath.Join("testdata", "mock_server.go")},
		TimeoutSec: 10, EnabledTools: []string{"echo"},
	}})
	m.CloseAll()

	if _, err := m.Start(context.Background(), "mock"); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("start after CloseAll: err=%v", err)
	}
	if ids := m.RunningIDs(); len(ids) != 0 {
		t.Fatalf("session appeared after shutdown: %v", ids)
	}
}
