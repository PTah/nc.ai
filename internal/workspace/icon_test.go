package workspace

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math/rand"
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

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodeIconDataURL(t *testing.T, got string) []byte {
	t.Helper()
	i := strings.Index(got, ";base64,")
	if !strings.HasPrefix(got, "data:image/") || i < 0 {
		t.Fatalf("not an image data URL: %q", got)
	}
	raw, err := base64.StdEncoding.DecodeString(got[i+len(";base64,"):])
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// Web app (Flask-like): icon lives in a package static/ folder — found by the
// well-known-path pass, without needing the templates.
func TestFindIconDataURL_WebStaticDir(t *testing.T) {
	root := t.TempDir()
	logo := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 0, 0, 0, 0}
	mustWrite(t, filepath.Join(root, "app", "static", "logo.png"), logo)

	got := decodeIconDataURL(t, FindIconDataURL(root))
	if len(got) != len(logo) {
		t.Fatalf("decoded len %d want %d", len(got), len(logo))
	}
}

// Brand logos in a static/ folder win over favicons (that is the mark users see
// in the web header).
func TestFindIconDataURL_PrefersLogoOverFavicon(t *testing.T) {
	root := t.TempDir()
	logo := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 1, 2, 3, 4}
	mustWrite(t, filepath.Join(root, "app", "static", "logo.png"), logo)
	mustWrite(t, filepath.Join(root, "app", "static", "favicon.ico"), []byte{0, 0, 1, 0})
	mustWrite(t, filepath.Join(root, "app", "templates", "base.html"),
		[]byte(`<link rel="icon" href="/static/favicon.ico?v=1">`))

	if got := decodeIconDataURL(t, FindIconDataURL(root)); len(got) != len(logo) {
		t.Fatalf("decoded len %d want %d (logo)", len(got), len(logo))
	}
}

// Custom layout: the icon is only reachable through a template reference, and
// the reference carries a cache-busting query string.
func TestFindIconDataURL_FromHTMLReference(t *testing.T) {
	root := t.TempDir()
	ico := []byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x10, 0x10}
	mustWrite(t, filepath.Join(root, "svc", "static", "favicon.ico"), ico)
	html := `<!doctype html><html><head>` +
		`<link rel="icon" type="image/png" href="/static/favicon.ico?v=0.9.42">` +
		`</head><body></body></html>`
	mustWrite(t, filepath.Join(root, "templates", "base.html"), []byte(html))

	got := decodeIconDataURL(t, FindIconDataURL(root))
	if len(got) != len(ico) {
		t.Fatalf("decoded len %d want %d (ico)", len(got), len(ico))
	}
}

// Desktop app icons still beat web logos.
func TestFindIconDataURL_BuildAppiconBeatsWebLogo(t *testing.T) {
	root := t.TempDir()
	appicon := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0}
	webLogo := append(append([]byte{}, appicon...), 9, 9, 9, 9)
	mustWrite(t, filepath.Join(root, "build", "appicon.png"), appicon)
	mustWrite(t, filepath.Join(root, "app", "static", "logo.png"), webLogo)

	if got := decodeIconDataURL(t, FindIconDataURL(root)); len(got) != len(appicon) {
		t.Fatalf("decoded len %d want %d (appicon)", len(got), len(appicon))
	}
}

// Remote and inline (data:) references must never be turned into a file read.
func TestFindIconDataURL_IgnoresExternalRefs(t *testing.T) {
	root := t.TempDir()
	html := `<link rel="icon" href="https://cdn.example.com/favicon.ico">` +
		`<img src="//cdn.example.com/logo.png">` +
		`<img src="data:image/png;base64,AAAA">` +
		`<img src="/static/../../../etc/passwd">`
	mustWrite(t, filepath.Join(root, "templates", "base.html"), []byte(html))
	if got := FindIconDataURL(root); got != "" {
		t.Fatalf("external/traversal refs must be ignored, got %q", got)
	}
}

// A real 600x600 PNG (too big to embed as-is) must be downscaled, not skipped.
func TestFindIconDataURL_DownscalesLargeRaster(t *testing.T) {
	root := t.TempDir()
	src := image.NewRGBA(image.Rect(0, 0, 600, 600))
	rnd := rand.New(rand.NewSource(1))
	for i := range src.Pix {
		src.Pix[i] = byte(rnd.Intn(256))
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	if buf.Len() <= maxIconBytes {
		t.Skipf("source PNG too small to matter (%d bytes)", buf.Len())
	}
	mustWrite(t, filepath.Join(root, "build", "appicon.png"), buf.Bytes())

	got := FindIconDataURL(root)
	if got == "" {
		t.Fatal("large app icon was skipped instead of downscaled")
	}
	if len(got) > 32*1024 {
		t.Fatalf("downscaled icon data URL is still %d bytes", len(got))
	}
	img, _, err := image.Decode(bytes.NewReader(decodeIconDataURL(t, got)))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() > iconPixels || b.Dy() > iconPixels {
		t.Fatalf("icon not downscaled: %dx%d", b.Dx(), b.Dy())
	}
}
