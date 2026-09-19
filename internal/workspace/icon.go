package workspace

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// maxIconBytes caps a non-raster icon (SVG/ICO/WebP) embedded as a data URL.
	maxIconBytes = 512 * 1024
	// maxDecodeBytes caps a raster file we are willing to decode + downscale.
	maxDecodeBytes = 8 * 1024 * 1024
	// iconPixels is the target edge for raster icons (rendered at ~18-24 px).
	iconPixels = 64
	// maxHTMLBytes caps one template read while hunting for an icon reference.
	maxHTMLBytes = 512 * 1024
)

// iconFileNames is the filename priority. Desktop app icons (Wails) come first,
// then brand marks of web apps, then favicons / PWA icons.
var iconFileNames = []string{
	"appicon.png",
	"appicon.ico",
	"icon.svg",
	"icon.png",
	"logo.svg",
	"logo.png",
	"apple-touch-icon.png",
	"favicon.svg",
	"favicon.png",
	"favicon.ico",
	"icon-512.png",
	"icon-192.png",
	"icon-128.png",
	"brand.svg",
	"brand.png",
}

// iconDirs are probed (in order) under the project root for iconFileNames.
// Covers Wails/desktop builds and common web static layouts: plain static/,
// Flask/Django app/static/, Vue/React public/ and src/assets/, etc.
var iconDirs = []string{
	"",
	"build",
	"build/windows",
	"public",
	"static",
	"app/static",
	"app/public",
	"src/static",
	"src/assets",
	"assets",
	"assets/images",
	"images",
	"img",
	"media",
	"resources",
	"frontend/public",
	"web/static",
	"server/static",
}

// htmlEntryDirs/htmlEntryPoints are templates probed for an icon reference when
// the well-known paths above came up empty.
var htmlEntryDirs = []string{
	"app/templates",
	"templates",
	"",
	"public",
	"src",
	"app",
	"web",
	"frontend",
}

var htmlEntryPoints = []string{
	"base.html",
	"layout.html",
	"index.html",
	"app.html",
	"main.html",
	"home.html",
	"login.html",
}

var (
	reHTMLTag  = regexp.MustCompile(`(?is)<(?:link|img)\b[^>]*>`)
	reHTMLAttr = regexp.MustCompile(`(?is)(?:href|src)\s*=\s*["']([^"']+)["']`)
	reHTMLRel  = regexp.MustCompile(`(?is)rel\s*=\s*["']([^"']*)["']`)
	reImageRef = regexp.MustCompile(`(?i)\.(?:png|jpe?g|gif|webp|svg|ico)(?:$|[?#])`)
)

// FindIconDataURL looks for a project/app icon under root and returns a
// data:...;base64,... URL suitable for <img src>. Empty string if none found.
//
// Two passes: well-known paths first (desktop app icons, web static dirs), then
// a scan of a few HTML entry points for <link rel="icon">/brand <img> refs —
// so web projects with a custom layout are covered too.
func FindIconDataURL(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return ""
	}
	for _, name := range iconFileNames {
		for _, dir := range iconDirs {
			full := filepath.Join(root, filepath.FromSlash(dir), name)
			if url := readIconDataURL(full); url != "" {
				return url
			}
		}
	}
	return findIconFromHTML(root)
}

// findIconFromHTML reads a handful of templates and resolves the first icon or
// brand-mark reference that points at a real file inside the project.
func findIconFromHTML(root string) string {
	for _, dir := range htmlEntryDirs {
		for _, name := range htmlEntryPoints {
			full := filepath.Join(root, filepath.FromSlash(dir), name)
			st, err := os.Stat(full)
			if err != nil || st.IsDir() || st.Size() <= 0 || st.Size() > maxHTMLBytes {
				continue
			}
			data, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			for _, ref := range iconRefsFromHTML(string(data)) {
				if url := resolveIconRef(root, ref); url != "" {
					return url
				}
			}
		}
	}
	return ""
}

// iconRefsFromHTML returns candidate icon/logo URLs, most icon-like first:
// <link rel="…icon…"> / apple-touch-icon, then <img> whose source looks like a
// brand mark.
func iconRefsFromHTML(html string) []string {
	var icons, logos []string
	for _, tag := range reHTMLTag.FindAllString(html, -1) {
		m := reHTMLAttr.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		ref := strings.TrimSpace(m[1])
		if ref == "" {
			continue
		}
		low := strings.ToLower(ref)
		if !reImageRef.MatchString(ref) && !strings.Contains(low, "icon") {
			continue
		}
		rel := ""
		if rm := reHTMLRel.FindStringSubmatch(tag); rm != nil {
			rel = strings.ToLower(rm[1])
		}
		switch {
		case strings.Contains(rel, "icon"):
			icons = append(icons, ref)
		case strings.Contains(low, "logo") || strings.Contains(low, "brand"):
			logos = append(logos, ref)
		}
	}
	return append(icons, logos...)
}

// resolveIconRef maps a template reference ("/static/logo.png?v=1") to a real
// file: first relative to the project root, then inside each first-level dir —
// which is where Flask/Django keep "static" folders ("<app>/static/…").
func resolveIconRef(root, ref string) string {
	if strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "//") ||
		strings.Contains(ref, "://") {
		return ""
	}
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	rel := strings.TrimLeft(strings.TrimSpace(ref), "/")
	if rel == "" || strings.Contains(rel, "..") {
		return ""
	}
	rel = filepath.FromSlash(rel)
	if url := readIconDataURL(filepath.Join(root, rel)); url != "" {
		return url
	}
	for _, dir := range firstLevelDirs(root) {
		if url := readIconDataURL(filepath.Join(root, dir, rel)); url != "" {
			return url
		}
	}
	return ""
}

// firstLevelDirs lists sub-directories of root that may hold a static folder.
func firstLevelDirs(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := make([]string, 0, 16)
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		switch e.Name() {
		case "node_modules", "__pycache__", "venv", ".venv", "dist", "data", "logs", "tests":
			continue
		}
		out = append(out, e.Name())
		if len(out) >= 24 {
			break
		}
	}
	return out
}

func readIconDataURL(path string) string {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Size() <= 0 {
		return ""
	}
	// Raster icons are decoded and downscaled: source files are often
	// 1000x1000 (hundreds of KB), while the UI renders them at ~18 px.
	if isRasterExt(filepath.Ext(path)) && st.Size() <= maxDecodeBytes {
		if url := downscaledDataURL(path); url != "" {
			return url
		}
	}
	if st.Size() > maxIconBytes {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	mime := iconMIME(path, data)
	if mime == "" {
		return ""
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func isRasterExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}

// downscaledDataURL decodes a raster image, shrinks it to iconPixels and returns
// a small PNG data URL. Empty string when the file is not a decodable image.
func downscaledDataURL(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	out := fitIcon(src)
	if out == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil || buf.Len() == 0 || buf.Len() > maxIconBytes {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// fitIcon box-averages src down to at most iconPixels on the longer edge.
// Returns src unchanged when it is already small enough.
func fitIcon(src image.Image) image.Image {
	if src == nil {
		return nil
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	if w <= iconPixels && h <= iconPixels {
		return src
	}
	scale := math.Min(float64(iconPixels)/float64(w), float64(iconPixels)/float64(h))
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0 := b.Min.Y + y*h/nh
		y1 := b.Min.Y + (y+1)*h/nh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < nw; x++ {
			x0 := b.Min.X + x*w/nw
			x1 := b.Min.X + (x+1)*w/nw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r += uint64(cr)
					g += uint64(cg)
					bl += uint64(cb)
					a += uint64(ca)
					n++
				}
			}
			if n == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(r / n >> 8),
				G: uint8(g / n >> 8),
				B: uint8(bl / n >> 8),
				A: uint8(a / n >> 8),
			})
		}
	}
	return dst
}

func iconMIME(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	}
	// Sniff magic headers when the extension is missing/odd.
	if len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg"
	}
	if len(data) >= 4 && string(data[:4]) == "RIFF" {
		return "image/webp"
	}
	if len(data) >= 4 && (data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 && data[3] == 0x00) {
		return "image/x-icon"
	}
	head := strings.ToLower(string(data))
	if i := strings.IndexByte(head, '>'); i >= 0 && i < 512 {
		head = head[:i]
	} else if len(head) > 512 {
		head = head[:512]
	}
	if strings.Contains(head, "<svg") {
		return "image/svg+xml"
	}
	return ""
}
