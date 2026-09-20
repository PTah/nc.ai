package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.6.28", "0.6.28", 0},
		{"0.6.28", "0.6.28.0", 0},
		{"v0.6.28", "0.6.27", 1},
		{"0.6.9", "0.6.10", -1},
		{"0.7.0", "0.6.99", 1},
		{"1.0.0", "0.9.9", 1},
		{"0.6.28", "", 1},
		{"0.6.28-rc1", "0.6.28", 0},
		{"0.6.28", "0.6.28.1", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestPickAsset(t *testing.T) {
	assets := []Asset{
		{Name: "nc.ai-0.6.30-macos-arm64.zip", URL: "u1"},
		{Name: "nc.ai-0.6.30-macos-universal.zip", URL: "u2"},
		{Name: "nc.ai-0.6.30-windows-amd64.zip", URL: "u3"},
	}

	if a, ok := PickAsset(assets, "windows", "amd64"); !ok || a.Name != "nc.ai-0.6.30-windows-amd64.zip" {
		t.Fatalf("windows: %v %v", a.Name, ok)
	}
	if a, ok := PickAsset(assets, "darwin", "arm64"); !ok || a.Name != "nc.ai-0.6.30-macos-arm64.zip" {
		t.Fatalf("darwin/arm64: %v %v", a.Name, ok)
	}
	if a, ok := PickAsset(assets, "darwin", "amd64"); !ok || a.Name != "nc.ai-0.6.30-macos-universal.zip" {
		t.Fatalf("darwin/amd64: %v %v", a.Name, ok)
	}
	// arm64 без отдельного ассета — берём universal.
	onlyUniversal := []Asset{{Name: "nc.ai-0.6.30-macos-universal.zip", URL: "u2"}}
	if a, ok := PickAsset(onlyUniversal, "darwin", "arm64"); !ok || a.Name != "nc.ai-0.6.30-macos-universal.zip" {
		t.Fatalf("universal fallback: %v %v", a.Name, ok)
	}
	if _, ok := PickAsset(assets, "linux", "amd64"); ok {
		t.Fatal("linux must not match")
	}
}

func TestParseSha256AndSidecar(t *testing.T) {
	sum := strings.Repeat("aB", 32) // 64 hex-символа
	if got := ParseSha256(sum + "  nc.ai-0.6.30-windows-amd64.zip\n"); got != strings.ToLower(sum) {
		t.Fatalf("parse: %q", got)
	}
	if got := ParseSha256("мусор без хеша"); got != "" {
		t.Fatalf("мусор: %q", got)
	}
	assets := []Asset{
		{Name: "nc.ai-0.6.30-windows-amd64.zip", URL: "u1"},
		{Name: "nc.ai-0.6.30-windows-amd64.zip.sha256", URL: "h1"},
	}
	if a, ok := PickSidecar(assets, "nc.ai-0.6.30-windows-amd64.zip"); !ok || a.URL != "h1" {
		t.Fatalf("sidecar: %v %v", a.Name, ok)
	}
	if _, ok := PickSidecar(assets, "nc.ai-0.6.29-windows-amd64.zip"); ok {
		t.Fatal("не должно быть sidecar для другого архива")
	}
}

func TestExecutableHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.bin")
	if err := os.WriteFile(path, []byte("notcursor"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ExecutableHash(path)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if len(got) != 64 {
		t.Fatalf("длина хеша: %d", len(got))
	}
	again, err := ExecutableHash(path)
	if err != nil || got != again {
		t.Fatalf("хеш нестабилен: %v", err)
	}
	if _, err := ExecutableHash(filepath.Join(dir, "нет-файла")); err == nil {
		t.Fatal("ожидали ошибку для отсутствующего файла")
	}
}

func TestSafeVersion(t *testing.T) {
	if got := safeVersion("0.6.30"); got != "0.6.30" {
		t.Fatalf("got %q", got)
	}
	if got := safeVersion("../evil/1"); got != "evil1" {
		t.Fatalf("got %q", got)
	}
	if got := safeVersion(""); got != "next" {
		t.Fatalf("got %q", got)
	}
}
