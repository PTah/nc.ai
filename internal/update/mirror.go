// Created by nc.ai 0.8.5.2 on macbook 2026-10-12 20:39:12
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Mirror — необязательный личный источник релизов: домашний Gitea владельца
// (или любой другой Gitea/GitHub-совместимый релизный API). Нужен, чтобы в своей
// сети качать 7 МБ за секунду вместо минут с GitHub; у остальных пользователей
// зеркала нет, и всё работает как раньше — только GitHub.
//
// Адреса зеркала и токен живут в локальных настройках машины (токен — в
// системном хранилище секретов), в репозиторий и в публичную сборку не попадают.
type Mirror struct {
	// BaseURL — корень репозитория на зеркале, например
	// http://192.168.x.x:3000/PapaTramp/nc.ai
	BaseURL string
	// Owner/Repo — можно не задавать: тогда берутся из последних двух сегментов BaseURL.
	Owner string
	Repo  string
	// Token — необязательный токен для приватного зеркала (Authorization: token …).
	Token string
}

// Источники релизов — для Info.Source и логов.
const (
	SourceGitHub = "github"
	SourceMirror = "mirror"
)

var (
	mirrorMu  sync.RWMutex
	mirrorCur *Mirror
)

// ConfigureMirror включает зеркало (nil или пустой BaseURL — только GitHub).
// Вызывается на старте приложения и при сохранении настроек.
func ConfigureMirror(m *Mirror) {
	mirrorMu.Lock()
	defer mirrorMu.Unlock()
	if m == nil || strings.TrimSpace(m.BaseURL) == "" {
		mirrorCur = nil
		return
	}
	clean := Mirror{
		BaseURL: strings.TrimRight(strings.TrimSpace(m.BaseURL), "/"),
		Owner:   strings.Trim(strings.TrimSpace(m.Owner), "/"),
		Repo:    strings.Trim(strings.TrimSpace(m.Repo), "/"),
		Token:   strings.TrimSpace(m.Token),
	}
	if clean.Owner == "" || clean.Repo == "" {
		if owner, repo, ok := splitOwnerRepo(clean.BaseURL); ok {
			clean.Owner, clean.Repo = owner, repo
			clean.BaseURL = strings.TrimSuffix(clean.BaseURL, "/"+owner+"/"+repo)
		}
	}
	if clean.Owner == "" || clean.Repo == "" || clean.BaseURL == "" {
		mirrorCur = nil
		return
	}
	mirrorCur = &clean
}

// MirrorEnabled сообщает, настроено ли зеркало (для интерфейса и логов).
func MirrorEnabled() bool {
	mirrorMu.RLock()
	defer mirrorMu.RUnlock()
	return mirrorCur != nil
}

func currentMirror() *Mirror {
	mirrorMu.RLock()
	defer mirrorMu.RUnlock()
	return mirrorCur
}

// splitOwnerRepo достаёт владельца и репозиторий из URL вида
// http://host:3000/Owner/Repo.
func splitOwnerRepo(base string) (string, string, bool) {
	u, err := url.Parse(base)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner, repo := parts[len(parts)-2], parts[len(parts)-1]
	if owner == "" || repo == "" {
		return "", "", false
	}
	return owner, repo, true
}

// mirrorHost — хост зеркала (для проверки, наши ли это запросы).
func mirrorHost(m *Mirror) string {
	u, err := url.Parse(m.BaseURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Host)
}

// authorizeMirror добавляет токен зеркала, если запрос идёт на его хост.
// Публичному зеркалу (как репозиторий владельца) токен не нужен.
func authorizeMirror(req *http.Request) {
	m := currentMirror()
	if m == nil || m.Token == "" || req == nil || req.URL == nil {
		return
	}
	if !strings.EqualFold(req.URL.Host, mirrorHost(m)) {
		return
	}
	req.Header.Set("Authorization", "token "+m.Token)
}

// mirrorAPI собирает адрес релизного API зеркала.
func mirrorAPI(m *Mirror, tail string) string {
	return fmt.Sprintf("%s/api/v1/repos/%s/%s/releases%s", m.BaseURL, url.PathEscape(m.Owner), url.PathEscape(m.Repo), tail)
}

// mirrorDownloadURL — ссылка на файл релиза, собранная от адреса зеркала.
// У Gitea browser_download_url строится из её ROOT_URL и может указывать на
// публичный домен вместо быстрого LAN-адреса, поэтому ссылки переписываем сами:
// {хост}/{owner}/{repo}/releases/download/{tag}/{name}.
func mirrorDownloadURL(m *Mirror, tag, name string) string {
	return fmt.Sprintf("%s/%s/%s/releases/download/%s/%s",
		m.BaseURL, url.PathEscape(m.Owner), url.PathEscape(m.Repo), url.PathEscape(tag), url.PathEscape(name))
}

// rewriteMirrorAssets переводит ссылки ассетов на адрес зеркала.
func rewriteMirrorAssets(m *Mirror, rel *Release) {
	for i := range rel.Assets {
		if strings.TrimSpace(rel.Assets[i].Name) == "" {
			continue
		}
		rel.Assets[i].URL = mirrorDownloadURL(m, rel.Tag, rel.Assets[i].Name)
	}
}

// latestFromMirror берёт последний релиз с зеркала. Gitea отдаёт тот же JSON,
// что GitHub, поэтому структуры общие; отдельно разбираемся только со ссылками.
func latestFromMirror(ctx context.Context, m *Mirror) (*Release, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if m == nil {
		return nil, errors.New("зеркало не настроено")
	}
	actx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	rel, err := mirrorRelease(actx, m, mirrorAPI(m, "/latest"))
	if err == nil {
		return rel, nil
	}
	// /releases/latest может быть недоступен (старая версия Gitea) — берём
	// список и выбираем самый свежий непустой релиз.
	list, lerr := mirrorReleaseList(actx, m)
	if lerr != nil {
		return nil, err
	}
	if list == nil {
		return nil, errors.New("зеркало: нет опубликованных релизов")
	}
	return list, nil
}

func mirrorRelease(ctx context.Context, m *Mirror, apiURL string) (*Release, error) {
	res, err := getWithRetry(ctx, apiURL, "application/json")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("зеркало: HTTP %d", res.StatusCode)
	}
	return decodeRelease(res.Body, m)
}

func mirrorReleaseList(ctx context.Context, m *Mirror) (*Release, error) {
	res, err := getWithRetry(ctx, mirrorAPI(m, "?limit=10"), "application/json")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("зеркало: HTTP %d", res.StatusCode)
	}
	var list []Release
	data, err := io.ReadAll(io.LimitReader(res.Body, maxReleaseJSON))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	var best *Release
	for i := range list {
		rel := list[i]
		if rel.Draft || rel.Prerelease || rel.Tag == "" {
			continue
		}
		if best == nil || CompareVersions(rel.Tag, best.Tag) > 0 {
			cp := rel
			best = &cp
		}
	}
	if best == nil {
		return nil, nil
	}
	rewriteMirrorAssets(m, best)
	return best, nil
}

func decodeRelease(body io.Reader, m *Mirror) (*Release, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxReleaseJSON))
	if err != nil {
		return nil, err
	}
	var rel Release
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, err
	}
	if rel.Tag == "" {
		return nil, errors.New("зеркало: пустой tag_name")
	}
	if rel.HTMLURL == "" {
		rel.HTMLURL = fmt.Sprintf("%s/releases/tag/%s", m.BaseURL, url.PathEscape(rel.Tag))
	}
	rewriteMirrorAssets(m, &rel)
	return &rel, nil
}
