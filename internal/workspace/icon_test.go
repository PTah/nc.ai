package workspace

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindIconDataURL_None(t *testing.T) {
	root := t.TempDir()
	if got := FindIconDataURL(root); got != "" {
		t.Fatalf("expected empty, got %q", got[:min(40, len(got))])
	}
	if got := FindIconDataURL(""); got != "" {
		t.Fatalf("empty root: got %q", got)
	}
}

func TestFindIconDataURL_PrefersBuildAppicon(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Minimal 1x1 PNG
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xff, 0xff, 0x3f,
		0x00, 0x05, 0xfe, 0x02, 0xfe, 0xa7, 0x35, 0x81,
		0x84, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(filepath.Join(root, "icon.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "appicon.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	got := FindIconDataURL(root)
	wantPrefix := "data:image/png;base64,"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("prefix = %q", got[:min(30, len(got))])
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, wantPrefix))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != len(png) {
		t.Fatalf("decoded len %d want %d", len(raw), len(png))
	}
}

func TestFindIconDataURL_SkipsOversized(t *testing.T) {
	root := t.TempDir()
	big := make([]byte, maxIconBytes+1)
	copy(big, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	if err := os.WriteFile(filepath.Join(root, "icon.png"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindIconDataURL(root); got != "" {
		t.Fatalf("oversized should be skipped, got len=%d", len(got))
	}
}

func TestOpenAttachesIcon(t *testing.T) {
	root := t.TempDir()
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0}
	if err := os.WriteFile(filepath.Join(root, "logo.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	p, err := m.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.IconURL, "data:image/png;base64,") {
		t.Fatalf("Open icon = %q", p.IconURL[:min(40, len(p.IconURL))])
	}
	list := m.List()
	if len(list) != 1 || list[0].IconURL == "" {
		t.Fatalf("List icon missing: %+v", list)
	}
}
