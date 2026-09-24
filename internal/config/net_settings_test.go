package config

import (
	"testing"
	"time"
)

func TestHTTPProtocolSetting(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.HTTPProtocolMode(); got != HTTPProtocolAuto {
		t.Fatalf("default mode = %q, want %q", got, HTTPProtocolAuto)
	}
	// Алиасы из UI/конфига нормализуются в известные режимы.
	for _, alias := range []string{"http/1.1", "HTTP1", " h1 "} {
		if err := s.SetHTTPProtocolMode(alias); err != nil {
			t.Fatal(err)
		}
		if got := s.HTTPProtocolMode(); got != HTTPProtocolHTTP11 {
			t.Fatalf("mode for %q = %q, want %q", alias, got, HTTPProtocolHTTP11)
		}
	}
	if err := s.SetHTTPProtocolMode("мусор"); err != nil {
		t.Fatal(err)
	}
	if got := s.HTTPProtocolMode(); got != HTTPProtocolAuto {
		t.Fatalf("unknown mode = %q, want %q", got, HTTPProtocolAuto)
	}
}

func TestHTTP2PingSetting(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.HTTP2Ping(); got != DefaultHTTP2PingSec*time.Second {
		t.Fatalf("default ping = %s, want %ds", got, DefaultHTTP2PingSec)
	}
	if err := s.SetHTTP2Ping(-1); err != nil {
		t.Fatal(err)
	}
	if got := s.HTTP2Ping(); got != 0 {
		t.Fatalf("ping off = %s, want 0", got)
	}
	if err := s.SetHTTP2Ping(1); err != nil {
		t.Fatal(err)
	}
	if got := s.HTTP2Ping(); got != minHTTP2PingSec*time.Second {
		t.Fatalf("ping too small = %s, want %ds", got, minHTTP2PingSec)
	}
	if err := s.SetHTTP2Ping(9999); err != nil {
		t.Fatal(err)
	}
	if got := s.HTTP2Ping(); got != maxHTTP2PingSec*time.Second {
		t.Fatalf("ping too big = %s, want %ds", got, maxHTTP2PingSec)
	}
}

func TestAgentRetrySettings(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentRetryCount(); got != DefaultAgentRetryCount {
		t.Fatalf("default retries = %d, want %d", got, DefaultAgentRetryCount)
	}
	if got := s.AgentRetryBudget(); got != DefaultAgentRetryBudgetSec*time.Second {
		t.Fatalf("default budget = %s, want %ds", got, DefaultAgentRetryBudgetSec)
	}

	if err := s.SetAgentRetryCount(25); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentRetryCount(); got != maxAgentRetryCount {
		t.Fatalf("clamped retries = %d, want %d", got, maxAgentRetryCount)
	}
	if err := s.SetAgentRetryCount(0); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentRetryCount(); got != DefaultAgentRetryCount {
		t.Fatalf("zero retries = %d, want default %d", got, DefaultAgentRetryCount)
	}

	if err := s.SetAgentRetryBudgetSec(5000); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentRetryBudget(); got != maxAgentRetryBudgetSec*time.Second {
		t.Fatalf("clamped budget = %s, want %ds", got, maxAgentRetryBudgetSec)
	}
	if err := s.SetAgentRetryBudgetSec(300); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentRetryBudget(); got != 5*time.Minute {
		t.Fatalf("budget = %s, want 5m", got)
	}
}
