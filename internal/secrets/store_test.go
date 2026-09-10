package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileBackendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := OpenFileOnly(dir)
	if err := s.Set(IDDeepSeek, "sk-test-1"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(IDDeepSeek)
	if err != nil || got != "sk-test-1" {
		t.Fatalf("get: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "secrets.json")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(IDDeepSeek); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(IDDeepSeek); err != ErrNotFound {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestSetEmptyDeletes(t *testing.T) {
	s := OpenFileOnly(t.TempDir())
	_ = s.Set(IDZai, "zai-key")
	if err := s.Set(IDZai, "  "); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(IDZai); err != ErrNotFound {
		t.Fatalf("empty set should delete: %v", err)
	}
}
