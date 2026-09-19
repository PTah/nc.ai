package workspace

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
)

// Max size for a project icon embedded into ListProjects (data URL).
const maxIconBytes = 512 * 1024

// Relative paths checked (first existing readable image wins).
// Covers Wails app icons, common web favicons/logos, and simple root icons.
var iconCandidates = []string{
	"build/appicon.png",
	"build/appicon.ico",
	"build/windows/icon.ico",
	"appicon.png",
	"appicon.ico",
	"icon.png",
	"icon.ico",
	"logo.png",
	"logo.svg",
	"favicon.ico",
	"favicon.png",
	"public/favicon.ico",
	"public/favicon.png",
	"public/icon.png",
	"public/logo.png",
	"assets/icon.png",
	"assets/logo.png",
	"frontend/public/favicon.ico",
	"frontend/public/favicon.png",
	"frontend/public/icon.png",
	"frontend/public/logo.png",
	"resources/icon.png",
	"Resources/icon.png",
}

// FindIconDataURL looks for a project/app icon under root and returns a
// data:...;base64,... URL suitable for <img src>. Empty string if none found.
func FindIconDataURL(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	for _, rel := range iconCandidates {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if url := readIconDataURL(full); url != "" {
			return url
		}
	}
	return ""
}

func readIconDataURL(path string) string {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() || st.Size() <= 0 || st.Size() > maxIconBytes {
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
	// Sniff a few magic headers when extension is missing/odd.
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
	trim := strings.TrimSpace(string(data))
	if strings.HasPrefix(trim, "<svg") || strings.HasPrefix(trim, "<?xml") {
		return "image/svg+xml"
	}
	return ""
}
