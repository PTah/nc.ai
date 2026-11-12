// Created by nc.ai 1.1.17 on new-admin-pc 2026-11-12 09:52:51
//go:build live

// Живая проверка OAuth на реальном hosted MCP-сервере (roadmap п.3):
//
//	go test -tags live ./internal/mcp -run LiveSuno -v -timeout 120s
//
// Проверяем всё, что можно без аккаунта: discovery (RFC 9728 → RFC 8414),
// динамическую регистрацию клиента и параметры authorize-ссылки. Само согласие
// пользователя в браузере тест не проходит — это остаётся ручным шагом.
package mcp

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLiveSunoOAuthDiscoveryAndAuthorizeURL(t *testing.T) {
	cfg := ServerConfig{
		ID: "suno", Label: "Suno", Enabled: true, Transport: "http",
		URL: "https://api.suno.com/mcp", TimeoutSec: 60,
	}
	var authURL string
	stop := errStopBeforeBrowserConsent
	_, err := AuthorizeServer(context.Background(), cfg, &memCreds{}, AuthOptions{
		Timeout: 60 * time.Second,
		OpenURL: func(raw string) error {
			authURL = raw
			return stop
		},
	})
	if err != nil && !strings.Contains(err.Error(), "stop-before-consent") {
		t.Fatalf("AuthorizeServer: %v", err)
	}
	if authURL == "" {
		t.Fatal("authorize-ссылка не собрана: discovery или регистрация клиента не прошли")
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatalf("разбор authorize-ссылки: %v", err)
	}
	q := u.Query()
	checks := []struct{ name, got, want string }{
		{"client_id", q.Get("client_id"), ""},
		{"code_challenge", q.Get("code_challenge"), ""},
		{"code_challenge_method", q.Get("code_challenge_method"), "S256"},
		{"state", q.Get("state"), ""},
		{"redirect_uri", q.Get("redirect_uri"), ""},
	}
	for _, c := range checks {
		if c.got == "" {
			t.Errorf("%s пустой в authorize-ссылке (%s)", c.name, u.Host+u.Path)
			continue
		}
		if c.want != "" && c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	t.Logf("authorize хост=%s client_id=%s resource=%q scope=%q",
		u.Host, q.Get("client_id"), q.Get("resource"), q.Get("scope"))
}

// errStopBeforeBrowserConsent обрывает прогон там, где начинается браузер.
type stopErr string

func (e stopErr) Error() string { return string(e) }

const errStopBeforeBrowserConsent = stopErr("stop-before-consent")
