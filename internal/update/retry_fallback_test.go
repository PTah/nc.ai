package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func withTestClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	old := Client
	Client = srv.Client()
	t.Cleanup(func() { Client = old })
}

func TestGetWithRetryRetriesServerErrors(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < apiAttempts {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	withTestClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := getWithRetry(ctx, srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if hits != apiAttempts {
		t.Fatalf("hits=%d want %d", hits, apiAttempts)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
}

func TestGetWithRetryGivesUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	withTestClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := getWithRetry(ctx, srv.URL, ""); err == nil {
		t.Fatal("expected error after retries")
	}
}

func TestSyntheticAssets(t *testing.T) {
	win := syntheticAssets("0.6.33", "windows", "amd64")
	if len(win) != 1 || win[0].Name != "nc.ai-0.6.33-windows-amd64.zip" {
		t.Fatalf("windows assets=%+v", win)
	}
	mac := syntheticAssets("0.6.33", "darwin", "arm64")
	if len(mac) != 2 || mac[0].Name != "nc.ai-0.6.33-macos-arm64.zip" {
		t.Fatalf("darwin assets=%+v", mac)
	}
}

func TestLatestTagFromParsesRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v0.6.33", http.StatusFound)
	}))
	defer srv.Close()
	withTestClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tag, err := latestTagFrom(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.6.33" {
		t.Fatalf("tag=%q", tag)
	}
}

func TestLatestTagFromRejectsPlainResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not a redirect"))
	}))
	defer srv.Close()
	withTestClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := latestTagFrom(ctx, srv.URL); err == nil {
		t.Fatal("expected error without Location")
	}
}

// Запасной путь: api.github.com недоступен, тег берём из HTML-редиректа,
// а ссылки на файлы достраиваем — так «Скачать и перезапустить» продолжает
// работать даже в сетях, где API GitHub заблокирован.
func TestReleaseByRedirectFrom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/redirect") {
			http.Redirect(w, r, "/releases/tag/v0.6.33", http.StatusFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	withTestClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rel, err := releaseByRedirectFrom(ctx, srv.URL+"/redirect", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.6.33" {
		t.Fatalf("tag=%q", rel.Tag)
	}
	a, ok := PickAsset(rel.Assets, "windows", "amd64")
	if !ok || !strings.HasSuffix(a.URL, "/v0.6.33/nc.ai-0.6.33-windows-amd64.zip") {
		t.Fatalf("asset=%+v ok=%v", a, ok)
	}
}
