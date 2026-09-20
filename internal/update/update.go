// Package update проверяет публичные релизы NotCursor.ai на GitHub и умеет
// скачать подходящий сборке файл, распаковать его и подменить работающую
// версию с перезапуском.
//
// Проверка делается по GitHub API (releases/latest) — без авторизации, поэтому
// на старте это одна попытка: ошибки сети тихо игнорируются, пользователю
// ничего не показываем, если проверка не удалась.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RepoSlug — публичное зеркало (оно же используется для релизов).
const RepoSlug = "PTah/nc.ai"

const (
	apiLatest = "https://api.github.com/repos/" + RepoSlug + "/releases/latest"
	// ReleasePage — куда попадёт пользователь, если предпочтёт скачать руками.
	ReleasePage = "https://github.com/" + RepoSlug + "/releases/latest"

	userAgent      = "NotCursor.ai-updater/1.0"
	maxReleaseJSON = 1 << 20 // 1 МБ на JSON релиза — с большим запасом
	maxAssetBytes  = 300 << 20
	assetTimeout   = 20 * time.Second
	downloadTick   = 250 * time.Millisecond
)

// Asset — один файл релиза.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release — нужное подмножество ответа GitHub.
type Release struct {
	Tag        string  `json:"tag_name"`
	Name       string  `json:"name"`
	HTMLURL    string  `json:"html_url"`
	Body       string  `json:"body"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Info — то, что нужно интерфейсу для предложения обновления.
type Info struct {
	Available  bool   `json:"available"`
	Current    string `json:"current"`
	Latest     string `json:"latest,omitempty"`
	Notes      string `json:"notes,omitempty"`
	URL        string `json:"url,omitempty"`
	AssetName  string `json:"assetName,omitempty"`
	AssetURL   string `json:"assetUrl,omitempty"`
	AssetSize  int64  `json:"assetSize,omitempty"`
	AssetFound bool   `json:"assetFound"`
	Error      string `json:"error,omitempty"`
}

// Client — HTTP-клиент с разумными таймаутами (проверка идёт на старте).
var Client = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   8 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

// ParseVersion раскладывает «v0.6.28.1» в срез чисел.
func ParseVersion(s string) []int {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if s == "" {
		return nil
	}
	if i := strings.IndexAny(s, "-+"); i >= 0 { // отрезаем -rc1, +build
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// CompareVersions возвращает -1 / 0 / 1 (a меньше / равна / больше b).
// Пропущенные части считаются нулями, поэтому «0.6.28» == «0.6.28.0».
func CompareVersions(a, b string) int {
	av, bv := ParseVersion(a), ParseVersion(b)
	n := len(av)
	if len(bv) > n {
		n = len(bv)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

// AssetSuffixes возвращает подходящие окончания имён файлов релиза по платформе,
// в порядке предпочтения (например, «-windows-amd64.zip»).
func AssetSuffixes(goos, goarch string) []string {
	switch goos {
	case "windows":
		if goarch == "386" {
			return []string{"-windows-386.zip", "-windows-amd64.zip"}
		}
		return []string{"-windows-amd64.zip"}
	case "darwin":
		if goarch == "arm64" {
			return []string{"-macos-arm64.zip", "-macos-universal.zip"}
		}
		return []string{"-macos-universal.zip", "-macos-amd64.zip"}
	}
	return nil
}

// PickAsset выбирает файл релиза, подходящий текущей платформе.
func PickAsset(assets []Asset, goos, goarch string) (Asset, bool) {
	for _, suffix := range AssetSuffixes(goos, goarch) {
		for _, a := range assets {
			if strings.HasSuffix(strings.ToLower(a.Name), suffix) && a.URL != "" {
				return a, true
			}
		}
	}
	return Asset{}, false
}

// LatestRelease забирает последний опубликованный релиз (без черновиков и пре-релизов).
func LatestRelease(ctx context.Context) (*Release, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	actx, cancel := context.WithTimeout(ctx, assetTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodGet, apiLatest, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	res, err := Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub API: HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxReleaseJSON))
	if err != nil {
		return nil, err
	}
	var rel Release
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, err
	}
	if rel.Tag == "" {
		return nil, errors.New("GitHub API: пустой tag_name")
	}
	return &rel, nil
}

// Check сравнивает текущую версию с последним релизом и подбирает файл.
// Ошибка сети возвращается как есть — вызывающий решает, показывать ли её.
func Check(ctx context.Context, current, goos, goarch string) (Info, error) {
	info := Info{Current: current, URL: ReleasePage}
	rel, err := LatestRelease(ctx)
	if err != nil {
		info.Error = err.Error()
		return info, err
	}
	info.Latest = strings.TrimPrefix(rel.Tag, "v")
	info.Notes = strings.TrimSpace(rel.Body)
	if rel.HTMLURL != "" {
		info.URL = rel.HTMLURL
	}
	if CompareVersions(info.Latest, current) <= 0 {
		return info, nil
	}
	info.Available = true
	if a, ok := PickAsset(rel.Assets, goos, goarch); ok {
		info.AssetFound = true
		info.AssetName = a.Name
		info.AssetURL = a.URL
		info.AssetSize = a.Size
	}
	return info, nil
}

// Download сохраняет файл релиза в dest, сообщая о прогрессе (если задан колбэк).
func Download(ctx context.Context, url, dest string, progress func(done, total int64)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("загрузка обновления: HTTP %d", res.StatusCode)
	}
	total := res.ContentLength
	if total <= 0 || total > maxAssetBytes {
		total = 0 // неизвестный/подозрительно большой — ограничимся лимитом чтения
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 256<<10)
	var done int64
	lastTick := time.Now()
	reader := io.LimitReader(res.Body, maxAssetBytes)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if progress != nil && time.Since(lastTick) >= downloadTick {
				lastTick = time.Now()
				progress(done, total)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if progress != nil {
		progress(done, total)
	}
	if total > 0 && done < total {
		return fmt.Errorf("загружено %d из %d байт — файл неполный", done, total)
	}
	return nil
}
