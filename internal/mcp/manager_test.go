// Created by nc.ai 1.2.13 on Papasha 2026-11-14 22:24:00
package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// brokenConfig is a server that can never start: the command does not exist, so
// Start fails immediately and increments the crash-loop counter.
func brokenConfig(id string) ServerConfig {
	return ServerConfig{
		ID:           id,
		Label:        id,
		Enabled:      true,
		Transport:    "stdio",
		Command:      fmt.Sprintf("ncai-no-such-binary-%d", os.Getpid()),
		TimeoutSec:   5,
		EnabledTools: []string{"echo"},
	}
}

// makeMockServerConfig is the working stdio server from testdata (initialize +
// tools/list + echo), used when a test needs a real successful start.
func makeMockServerConfig(t *testing.T) ServerConfig {
	t.Helper()
	return ServerConfig{
		ID:           "mock",
		Label:        "Mock",
		Enabled:      true,
		Transport:    "stdio",
		Command:      goBin(),
		Args:         []string{"run", "testdata/mock_server.go"},
		TimeoutSec:   20,
		EnabledTools: []string{"echo"},
	}
}

func TestRestartBackoffGrowsAndGivesUp(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	m.SetConfigs([]ServerConfig{brokenConfig("broken")})

	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute}
	for i, w := range want {
		m.failures["broken"] = i + 1
		m.lastFailed["broken"] = time.Now()
		if got := m.restartDelayLocked("broken"); got != w {
			t.Fatalf("failures=%d delay=%s want %s", i+1, got, w)
		}
	}

	// Right after a failure the automatic restart is refused, with a reason a
	// human can act on.
	m.failures["broken"] = 1
	m.lastFailed["broken"] = time.Now()
	if ok, reason := m.restartAllowed("broken"); ok || !strings.Contains(reason, "следующая попытка") {
		t.Fatalf("restartAllowed=%v reason=%q", ok, reason)
	}

	// Once the interval elapsed, the retry is allowed again.
	m.lastFailed["broken"] = time.Now().Add(-mcpMinRestartInterval - time.Second)
	if ok, reason := m.restartAllowed("broken"); !ok {
		t.Fatalf("retry after backoff refused: %s", reason)
	}

	// After mcpMaxAutoRestarts consecutive failures auto-restart stops for good.
	m.failures["broken"] = mcpMaxAutoRestarts
	m.lastFailed["broken"] = time.Now().Add(-time.Hour)
	if ok, reason := m.restartAllowed("broken"); ok || !strings.Contains(reason, "авто-перезапуск выключен") {
		t.Fatalf("limit not enforced: ok=%v reason=%q", ok, reason)
	}

	// An explicit start from the UI clears the penalty.
	m.ResetBackoff("broken")
	if ok, _ := m.restartAllowed("broken"); !ok {
		t.Fatal("ResetBackoff did not clear the backoff")
	}
}

// A command changed by the user must be retried at once instead of inheriting
// the penalty of the previous, broken command line.
func TestSetConfigsResetsBackoffOnLaunchChange(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()

	cfg := brokenConfig("srv")
	m.SetConfigs([]ServerConfig{cfg})
	m.mu.Lock()
	m.failures["srv"] = mcpMaxAutoRestarts
	m.lastFailed["srv"] = time.Now()
	m.mu.Unlock()

	// Same launch line: the penalty stays.
	m.SetConfigs([]ServerConfig{cfg})
	if ok, _ := m.restartAllowed("srv"); ok {
		t.Fatal("identical config must not reset the backoff")
	}

	// Edited command: backoff is cleared.
	changed := cfg
	changed.Command = "another-binary"
	m.SetConfigs([]ServerConfig{changed})
	if ok, reason := m.restartAllowed("srv"); !ok {
		t.Fatalf("changed command must reset the backoff: %s", reason)
	}
}

func TestEnsureStartedBacksOffCrashLoop(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	m.SetConfigs([]ServerConfig{brokenConfig("broken")})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	m.EnsureStarted(ctx)
	m.mu.Lock()
	fails := m.failures["broken"]
	state := m.states["broken"]
	m.mu.Unlock()
	if fails != 1 {
		t.Fatalf("failures=%d want 1", fails)
	}
	if state != "error" {
		t.Fatalf("state=%q want error", state)
	}

	// The second agent step must not respawn the crashing server.
	m.EnsureStarted(ctx)
	m.mu.Lock()
	fails = m.failures["broken"]
	m.mu.Unlock()
	if fails != 1 {
		t.Fatalf("crash-looping server was restarted under backoff: failures=%d", fails)
	}
}

// EnsureStartedBackground runs its checks while holding m.mu; the backoff check
// must use the locked variant or the manager deadlocks on the first agent step.
func TestEnsureStartedBackgroundDoesNotDeadlock(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	m.SetConfigs([]ServerConfig{brokenConfig("broken")})
	m.mu.Lock()
	m.failures["broken"] = mcpMaxAutoRestarts
	m.lastFailed["broken"] = time.Now()
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		m.EnsureStartedBackground()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("EnsureStartedBackground deadlocked on the manager mutex")
	}
}

// A call to a crash-looping server returns an explanation instead of spawning it
// again (which used to leave a new node.exe behind on every agent step).
func TestCallReportsBackoffInsteadOfRespawning(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	m.SetConfigs([]ServerConfig{brokenConfig("broken")})
	m.mu.Lock()
	m.failures["broken"] = mcpMaxAutoRestarts
	m.lastFailed["broken"] = time.Now()
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := m.Call(ctx, "mcp__broken__echo", `{"text":"hi"}`)
	if err == nil || !strings.Contains(err.Error(), "авто-перезапуск выключен") {
		t.Fatalf("err=%v", err)
	}
	m.mu.Lock()
	sess := m.sessions["broken"]
	m.mu.Unlock()
	if sess != nil {
		t.Fatal("call must not start a server that is under backoff")
	}
}

// A successful start clears the penalty, so one crash does not leave the server
// permanently limited.
func TestSuccessfulStartClearsFailures(t *testing.T) {
	m := NewManager()
	defer m.CloseAll()
	mock := makeMockServerConfig(t)
	m.SetConfigs([]ServerConfig{mock})
	m.mu.Lock()
	m.failures[mock.ID] = 2
	m.lastFailed[mock.ID] = time.Now()
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := m.Start(ctx, mock.ID); err != nil {
		t.Fatalf("start: %v", err)
	}
	m.mu.Lock()
	fails := m.failures[mock.ID]
	_, failed := m.lastFailed[mock.ID]
	m.mu.Unlock()
	if fails != 0 || failed {
		t.Fatalf("success did not clear failures (fails=%d failed=%v)", fails, failed)
	}
}
