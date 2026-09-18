package config

import "testing"

// TestAgentStepsUnlimitedAndWarn covers the "no step cap, warn after N" mode.
func TestAgentStepsUnlimitedAndWarn(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.MaxAgentSteps(); got != DefaultAgentMaxSteps {
		t.Fatalf("default MaxAgentSteps = %d, want %d", got, DefaultAgentMaxSteps)
	}
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentWarnSteps(); got != DefaultAgentWarnSteps {
		t.Fatalf("default AgentWarnSteps = %d, want %d", got, DefaultAgentWarnSteps)
	}
	// -1 = unlimited: the marker must survive a round trip (0 would mean "default").
	if err := s.SetAgentMaxSteps(-1); err != nil {
		t.Fatal(err)
	}
	if got := s.MaxAgentSteps(); got != -1 {
		t.Fatalf("MaxAgentSteps() = %d, want -1 (unlimited)", got)
	}
	// 0 means "default", not "unlimited".
	if err := s.SetAgentMaxSteps(0); err != nil {
		t.Fatal(err)
	}
	if got := s.MaxAgentSteps(); got != DefaultAgentMaxSteps {
		t.Fatalf("MaxAgentSteps() = %d, want default %d", got, DefaultAgentMaxSteps)
	}
	if err := s.SetAgentMaxSteps(300); err != nil {
		t.Fatal(err)
	}
	if got := s.MaxAgentSteps(); got != 300 {
		t.Fatalf("MaxAgentSteps() = %d, want 300", got)
	}
	// Warning threshold: -1 disables, positive is kept.
	if err := s.SetAgentWarnSteps(-1); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentWarnSteps(); got != -1 {
		t.Fatalf("AgentWarnSteps() = %d, want -1 (off)", got)
	}
	if err := s.SetAgentWarnSteps(250); err != nil {
		t.Fatal(err)
	}
	if got := s.AgentWarnSteps(); got != 250 {
		t.Fatalf("AgentWarnSteps() = %d, want 250", got)
	}
	// Values are clamped, never unbounded.
	if err := s.SetAgentMaxSteps(99999); err != nil {
		t.Fatal(err)
	}
	if got := s.MaxAgentSteps(); got != 5000 {
		t.Fatalf("MaxAgentSteps() = %d, want clamp to 5000", got)
	}
}
