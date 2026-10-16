// Created by nc.ai 0.8.5.2 on macbook 2026-10-12 20:41:56
package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigureMirrorParsesOwnerRepo(t *testing.T) {
	defer ConfigureMirror(nil)

	ConfigureMirror(&Mirror{BaseURL: "http://192.168.1.10:3000/PapaTramp/nc.ai/"})
	if !MirrorEnabled() {
		t.Fatal("зеркало должно быть включено")
	}
	m := currentMirror()
	if m.Owner != "PapaTramp" || m.Repo != "nc.ai" || m.BaseURL != "http://192.168.1.10:3000" {
		t.Fatalf("разбор зеркала = %+v", m)
	}

	ConfigureMirror(&Mirror{BaseURL: "   "})
	if MirrorEnabled() {
		t.Fatal("пустой адрес должен выключать зеркало")
	}
	ConfigureMirror(&Mirror{BaseURL: "http://host:3000"})
	if MirrorEnabled() {
		t.Fatal("адрес без owner/repo должен выключать зеркало")
	}
}

// Ссылки ассетов зеркала всегда переводим на его адрес: Gitea собирает
// browser_download_url из своего ROOT_URL и может отдать публичный домен вместо
// быстрого LAN-адреса.
func TestLatestFromMirrorRewritesAssetURLs(t *testing.T) {
	defer ConfigureMirror(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/PapaTramp/nc.ai/releases/latest" {
			http.NotFound(w, r)
			return
		}
		rel := Release{
			Tag:     "v0.8.5.3",
			Name:    "NotCursor 0.8.5.3",
			HTMLURL: "https://git.example/PapaTramp/nc.ai/releases/tag/v0.8.5.3",
			Assets: []Asset{
				{
					Name: "nc.ai-0.8.5.3-macos-arm64.zip",
					URL:  "https://git.example/PapaTramp/nc.ai/releases/download/v0.8.5.3/nc.ai-0.8.5.3-macos-arm64.zip",
					Size: 7080000,
				},
				{
					Name: "nc.ai-0.8.5.3-macos-arm64.zip.sha256",
					URL:  "https://git.example/PapaTramp/nc.ai/releases/download/v0.8.5.3/nc.ai-0.8.5.3-macos-arm64.zip.sha256",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(rel)
	}))
	defer srv.Close()

	ConfigureMirror(&Mirror{BaseURL: srv.URL + "/PapaTramp/nc.ai"})
	rel, err := latestFromMirror(context.Background(), currentMirror())
	if err != nil {
		t.Fatalf("latestFromMirror: %v", err)
	}
	if rel.Tag != "v0.8.5.3" {
		t.Fatalf("тег = %q", rel.Tag)
	}
	asset, ok := PickAsset(rel.Assets, "darwin", "arm64")
	if !ok {
		t.Fatal("ассет для darwin/arm64 не найден")
	}
	want := srv.URL + "/PapaTramp/nc.ai/releases/download/v0.8.5.3/" + asset.Name
	if asset.URL != want {
		t.Fatalf("ссылка ассета = %q, хотели %q", asset.URL, want)
	}
	if sc, ok := PickSidecar(rel.Assets, asset.Name); !ok || !strings.HasPrefix(sc.URL, srv.URL) {
		t.Fatalf("подпись релиза не переведена на зеркало: %+v", sc)
	}
}

// /releases/latest есть не во всех версиях Gitea — тогда берём список и выбираем
// самый свежий релиз.
func TestLatestFromMirrorFallsBackToList(t *testing.T) {
	defer ConfigureMirror(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			http.NotFound(w, r)
		case strings.HasSuffix(r.URL.Path, "/releases"):
			_ = json.NewEncoder(w).Encode([]Release{
				{Tag: "v0.8.5.2", Assets: []Asset{{Name: "nc.ai-0.8.5.2-macos-arm64.zip"}}},
				{Tag: "v0.8.6", Draft: true},
				{Tag: "v0.8.5.3", Assets: []Asset{{Name: "nc.ai-0.8.5.3-macos-arm64.zip"}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ConfigureMirror(&Mirror{BaseURL: srv.URL + "/PapaTramp/nc.ai"})
	rel, err := latestFromMirror(context.Background(), currentMirror())
	if err != nil {
		t.Fatalf("latestFromMirror: %v", err)
	}
	if rel.Tag != "v0.8.5.3" {
		t.Fatalf("выбран релиз %q, хотели свежий опубликованный v0.8.5.3", rel.Tag)
	}
}

func TestMirrorTokenSentWhenConfigured(t *testing.T) {
	defer ConfigureMirror(nil)

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(Release{Tag: "v1", Assets: nil})
	}))
	defer srv.Close()

	ConfigureMirror(&Mirror{BaseURL: srv.URL + "/PapaTramp/nc.ai", Token: "secret-token"})
	if _, err := latestFromMirror(context.Background(), currentMirror()); err != nil {
		t.Fatalf("latestFromMirror: %v", err)
	}
	if gotAuth != "token secret-token" {
		t.Fatalf("Authorization = %q, хотели token secret-token", gotAuth)
	}
}

// Короткий сбой своего Gitea (перезапуск, заливка релиза) не должен уводить
// обновление на GitHub: зеркало опрашиваем несколькими раундами.
func TestLatestFromMirrorRetriesBeforeGivingUp(t *testing.T) {
	defer ConfigureMirror(nil)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits <= 2 { // первый раунд: и /releases/latest, и список — мимо
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Release{Tag: "v0.8.7.8", Assets: []Asset{{Name: "nc.ai-0.8.7.8-macos-arm64.zip"}}})
	}))
	defer srv.Close()

	ConfigureMirror(&Mirror{BaseURL: srv.URL + "/PapaTramp/nc.ai"})
	rel, err := latestFromMirror(context.Background(), currentMirror())
	if err != nil {
		t.Fatalf("второй раунд должен был спасти: %v", err)
	}
	if rel.Tag != "v0.8.7.8" {
		t.Fatalf("тег = %q", rel.Tag)
	}
	if hits != 3 {
		t.Fatalf("запросов к зеркалу = %d, хотели 3 (2 неудачи + успех)", hits)
	}
}

// Фолбэк на GitHub обязан быть виден: без причины отказа зеркала он выглядит
// как «обновление почему-то качалось медленно» (инцидент 16.10.2026 22:14:54).
func TestLatestReleaseWithMirrorReportsMirrorError(t *testing.T) {
	defer ConfigureMirror(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.9.9","assets":[]}`))
	}))
	defer srv.Close()
	withTestClient(t, srv)
	oldLatest := apiLatest
	apiLatest = srv.URL + "/latest"
	t.Cleanup(func() { apiLatest = oldLatest })

	ConfigureMirror(&Mirror{BaseURL: srv.URL + "/PapaTramp/nc.ai"})
	rel, source, mirrorErr, err := latestReleaseWithMirror(context.Background(), "darwin", "arm64")
	if err != nil {
		t.Fatalf("latestReleaseWithMirror: %v", err)
	}
	if source != SourceGitHub {
		t.Fatalf("источник = %q, хотели %q", source, SourceGitHub)
	}
	if mirrorErr == nil {
		t.Fatal("причина отказа зеркала потеряна — фолбэк снова будет невидим")
	}
	if rel.Tag != "v0.9.9" {
		t.Fatalf("тег = %q", rel.Tag)
	}
}

// Check обязан донести причину отказа зеркала до интерфейса и лога.
func TestCheckSurfacesMirrorError(t *testing.T) {
	defer ConfigureMirror(nil)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Release{
			Tag:    "v0.9.9",
			Assets: []Asset{{Name: "nc.ai-0.9.9-macos-arm64.zip"}},
		})
	}))
	defer srv.Close()
	withTestClient(t, srv)
	oldLatest := apiLatest
	apiLatest = srv.URL + "/latest"
	t.Cleanup(func() { apiLatest = oldLatest })

	ConfigureMirror(&Mirror{BaseURL: srv.URL + "/PapaTramp/nc.ai"})
	info, err := Check(context.Background(), "0.8.7.8", "darwin", "arm64")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if info.Source != SourceGitHub {
		t.Fatalf("источник = %q", info.Source)
	}
	if info.MirrorError == "" {
		t.Fatal("info.MirrorError пуст — в логе и UI причина не появится")
	}
	if !info.Available {
		t.Fatal("новая версия с GitHub должна предлагаться к установке")
	}
}
