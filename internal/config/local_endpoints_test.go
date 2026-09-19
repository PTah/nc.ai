package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyLocalToEndpoints(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "activeProvider": "local",
  "localBaseUrl": "http://10.0.0.1:11434/v1",
  "localModel": "qwen2.5-coder:7b"
}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStoreForTest(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s.Provider(); got != "local:default" {
		t.Fatalf("provider=%q", got)
	}
	eps := s.LocalEndpoints()
	if len(eps) != 1 || eps[0].BaseURL != "http://10.0.0.1:11434/v1" || eps[0].Model != "qwen2.5-coder:7b" {
		t.Fatalf("endpoints=%+v", eps)
	}
	if s.LocalBaseURL() != "http://10.0.0.1:11434/v1" {
		t.Fatalf("base=%q", s.LocalBaseURL())
	}
}

func TestUpsertAndSwitchLocalEndpoints(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	ep, err := s.UpsertLocalEndpoint(LocalEndpoint{
		Name: "Ollama LAN", BaseURL: "http://10.0.0.1:11434/v1", Model: "llama3",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ep.ID == "" || ep.ID == DefaultLocalEndpointID {
		t.Fatalf("expected new id, got %q", ep.ID)
	}
	if err := s.SetActiveProvider(MakeLocalProvider(ep.ID)); err != nil {
		t.Fatal(err)
	}
	if s.LocalModel() != "llama3" {
		t.Fatalf("model=%q", s.LocalModel())
	}
	if err := s.SetLocalAPIKey("tok-lan"); err != nil {
		t.Fatal(err)
	}
	if s.APIKey(MakeLocalProvider(ep.ID)) != "tok-lan" {
		t.Fatal("secret not stored under local:<id>")
	}
	// Default endpoint keeps separate secret namespace.
	if err := s.SetActiveProvider(MakeLocalProvider(DefaultLocalEndpointID)); err != nil {
		t.Fatal(err)
	}
	if s.HasAPIKey(MakeLocalProvider(DefaultLocalEndpointID)) {
		t.Fatal("default should not share LAN token")
	}
}

func TestCannotRemoveLastLocalEndpoint(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	_ = s.Load()
	if err := s.RemoveLocalEndpoint(DefaultLocalEndpointID); err == nil {
		t.Fatal("expected error removing last endpoint")
	}
}

func TestNormalizeLocalProvider(t *testing.T) {
	if got := normalizeProvider("local"); got != "local:default" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeProvider("local:Home-GPU"); got != "local:home-gpu" {
		t.Fatalf("got %q", got)
	}
	if !IsLocalProvider("local:x") || IsLocalProvider("deepseek") {
		t.Fatal("IsLocalProvider")
	}
}
