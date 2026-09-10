package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateKeysOutOfSettingsJSON(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "deepseekApiKey": "sk-from-json",
  "zaiApiKey": "zai-from-json",
  "gitPassword": "should-wipe"
}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStoreForTest(dir)
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if s.APIKey(ProviderDeepSeek) != "sk-from-json" || s.APIKey(ProviderZAI) != "zai-from-json" {
		t.Fatalf("keys not migrated: ds=%q zai=%q", s.APIKey(ProviderDeepSeek), s.APIKey(ProviderZAI))
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-from-json") || strings.Contains(string(data), "zai-from-json") || strings.Contains(string(data), "should-wipe") {
		t.Fatalf("settings.json still has secrets:\n%s", data)
	}
	if !s.HasAPIKey(ProviderDeepSeek) {
		t.Fatal("HasAPIKey")
	}
}

func TestSetAndClearAPIKey(t *testing.T) {
	s := NewStoreForTest(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeepSeekAPIKey("sk-live"); err != nil {
		t.Fatal(err)
	}
	if s.ActiveAPIKey() != "sk-live" {
		t.Fatal(s.ActiveAPIKey())
	}
	if err := s.ClearAPIKey(ProviderDeepSeek); err != nil {
		t.Fatal(err)
	}
	if s.HasAPIKey(ProviderDeepSeek) {
		t.Fatal("cleared key still set")
	}
}

func TestSettingsJSONOmitsKeys(t *testing.T) {
	st := Settings{DeepSeekAPIKey: "sk-leak", DeepSeekModel: "deepseek-v4-flash"}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "sk-leak") {
		t.Fatalf("marshal leaked key: %s", b)
	}
}
