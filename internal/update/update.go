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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	// SameVersion — номер версии совпал, но сборка опубликована другая
	// (в релиз перезалили файл). Сравнение по хешу исполняемого файла.
	SameVersion  bool   `json:"sameVersion,omitempty"`
	BuildDiffers bool   `json:"buildDiffers,omitempty"`
	BuildHash    string `json:"buildHash,omitempty"`
	LocalHash    string `json:"localHash,omitempty"`
	Error        string `json:"error,omitempty"`
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

// Sha256SidecarSuffix — суффикс вспомогательного файла релиза: рядом с архивом
// кладётся <имя архива>.sha256 с sha256 главного исполняемого файла ВНУТРИ
// архива. GitHub отдаёт digest только самого архива, а сравнивать надо
// запускаемый бинарник — поэтому публикуем его отдельным маленьким файлом.
// Если файла нет, проверка работает как раньше (только по номеру версии).
const Sha256SidecarSuffix = ".sha256"

var sha256Token = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)

// ParseSha256 вытаскивает первый sha256-хеш из подписи (совместимо с sha256sum).
func ParseSha256(s string) string {
	if m := sha256Token.FindString(s); m != "" {
		return strings.ToLower(m)
	}
	return ""
}

// ExecutableHash считает sha256 файла.
func ExecutableHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var (
	localHashOnce sync.Once
	localHash     string
)

// LocalExeHash — sha256 работающего сейчас исполняемого файла (кэшируется: под
// нами он не меняется). На macOS это <bundle>/Contents/MacOS/NotCursor.
func LocalExeHash() string {
	localHashOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil && resolved != "" {
			exe = resolved
		}
		hash, err := ExecutableHash(exe)
		if err != nil {
			return
		}
		localHash = hash
	})
	return localHash
}

// PickSidecar ищет вспомогательный файл с хешем для конкретного архива.
func PickSidecar(assets []Asset, assetName string) (Asset, bool) {
	if strings.TrimSpace(assetName) == "" {
		return Asset{}, false
	}
	want := strings.ToLower(assetName + Sha256SidecarSuffix)
	for _, a := range assets {
		if strings.ToLower(strings.TrimSpace(a.Name)) == want && a.URL != "" {
			return a, true
		}
	}
	return Asset{}, false
}

// FetchSha256 скачивает подпись и возвращает хеш (пусто при любой ошибке).
func FetchSha256(ctx context.Context, url string) string {
	if url == "" {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	actx, cancel := context.WithTimeout(ctx, assetTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(actx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := Client.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096)) // подпись крошечная
	if err != nil {
		return ""
	}
	return ParseSha256(string(body))
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

// DecideAvailability: предлагать ли обновление.
// remoteVsLocal — CompareVersions(remote, local); buildDiffers — хеши
// разошлись. Хеш учитывается только при равных номерах версии: локальная
// сборка новее GitHub не должна звать «обновиться» из‑за другого sha256.
func DecideAvailability(remoteVsLocal int, buildDiffers bool) (available, sameVersion bool) {
	switch {
	case remoteVsLocal > 0:
		return true, false
	case remoteVsLocal == 0 && buildDiffers:
		return true, true
	default:
		return false, false
	}
}

// Check сравнивает текущую версию с последним релизом и подбирает файл.
// Ошибка сети возвращается как есть — вызывающий решает, показывать ли её.
func Check(ctx context.Context, current, goos, goarch string) (Info, error) {
	info := Info{Current: current, URL: ReleasePage, LocalHash: LocalExeHash()}
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
	// cmp: remote vs local — >0 remote newer, 0 same, <0 local ahead of GitHub.
	cmp := CompareVersions(info.Latest, current)

	if a, ok := PickAsset(rel.Assets, goos, goarch); ok {
		info.AssetFound = true
		info.AssetName = a.Name
		info.AssetURL = a.URL
		info.AssetSize = a.Size
		// Хеш сборки имеет смысл только при том же номере версии: иначе
		// локальная 0.6.33 и релиз 0.6.32 всегда «разные» и ложно зовут обновиться.
		if cmp == 0 {
			if sc, ok := PickSidecar(rel.Assets, a.Name); ok {
				if remote := FetchSha256(ctx, sc.URL); remote != "" {
					info.BuildHash = remote
					info.BuildDiffers = info.LocalHash != "" && info.LocalHash != remote
				}
			}
		}
	}
	info.Available, info.SameVersion = DecideAvailability(cmp, info.BuildDiffers)
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
