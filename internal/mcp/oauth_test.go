// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:38:43
// Edited by nc.ai 1.1.17 on new-admin-pc 2026-11-12 09:49:30
// Created by nc.ai 1.1.16 on macbook 2026-11-11
package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// memCreds — хранилище токенов в памяти для тестов.
type memCreds struct {
	mu    sync.Mutex
	items map[string]Credentials
}

func (m *memCreds) LoadMCPOAuth(id string) (Credentials, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.items[id]
	return c, ok
}

func (m *memCreds) SaveMCPOAuth(id string, c Credentials) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.items == nil {
		m.items = map[string]Credentials{}
	}
	m.items[id] = c
	return nil
}

func (m *memCreds) ClearMCPOAuth(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, id)
	return nil
}

// oauthTestAS — минимальный authorization server: discovery, регистрация
// клиента, обмен кода и refresh. MCP-эндпоинт требует Bearer из этого сервера.
type oauthTestAS struct {
	t         *testing.T
	srv       *httptest.Server
	mu        sync.Mutex
	verifier  string // code_verifier, который прислал клиент
	refreshed int
	registered int
	// noDCR убирает registration_endpoint из discovery — провайдер без
	// динамической регистрации клиента.
	noDCR bool
}

func (a *oauthTestAS) start() {
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		meta := map[string]any{
			"issuer":                           a.srv.URL,
			"authorization_endpoint":           a.srv.URL + "/authorize",
			"token_endpoint":                   a.srv.URL + "/token",
			"code_challenge_methods_supported": []string{"S256"},
			"scopes_supported":                 []string{"mcp"},
		}
		if !a.noDCR {
			meta["registration_endpoint"] = a.srv.URL + "/register"
		}
		_ = json.NewEncoder(w).Encode(meta)
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.registered++
		a.mu.Unlock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["token_endpoint_auth_method"] != "none" {
			a.t.Errorf("ожидался public client, получено %v", body["token_endpoint_auth_method"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "client-1"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			if r.Form.Get("code") != "code-1" {
				a.t.Errorf("code = %q", r.Form.Get("code"))
			}
			a.mu.Lock()
			a.verifier = r.Form.Get("code_verifier")
			a.mu.Unlock()
			if r.Form.Get("code_verifier") == "" {
				a.t.Error("PKCE: пустой code_verifier")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-1", "refresh_token": "ref-1",
				"token_type": "Bearer", "expires_in": 3600,
			})
		case "refresh_token":
			a.mu.Lock()
			a.refreshed++
			a.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-2", "token_type": "Bearer", "expires_in": 3600,
			})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		if method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result := map[string]any{}
		switch method {
		case "initialize":
			result = map[string]any{"protocolVersion": protocolVersion, "serverInfo": map[string]any{"name": "as"}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "ping", "inputSchema": map[string]any{"type": "object"}}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req["id"], "result": result})
	})
	a.srv = httptest.NewServer(mux)
}

func TestAuthorizeServer_PKCEFlowAndSessionWithToken(t *testing.T) {
	as := &oauthTestAS{t: t}
	as.start()
	defer as.srv.Close()

	cfg := ServerConfig{ID: "suno", Label: "Suno", Enabled: true, Transport: "http", URL: as.srv.URL + "/mcp", TimeoutSec: 30}
	src := &memCreds{}

	var authorizeSeen url.Values
	open := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		authorizeSeen = u.Query()
		if authorizeSeen.Get("code_challenge_method") != "S256" {
			t.Errorf("PKCE method = %q", authorizeSeen.Get("code_challenge_method"))
		}
		if authorizeSeen.Get("client_id") != "client-1" {
			t.Errorf("client_id = %q", authorizeSeen.Get("client_id"))
		}
		// Браузер пользователя: согласие → редирект на локальный callback.
		redirect := authorizeSeen.Get("redirect_uri")
		state := authorizeSeen.Get("state")
		go func() {
			time.Sleep(20 * time.Millisecond)
			resp, err := http.Get(redirect + "?code=code-1&state=" + url.QueryEscape(state))
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	creds, err := AuthorizeServer(t.Context(), cfg, src, AuthOptions{OpenURL: open, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("AuthorizeServer: %v", err)
	}
	if creds.AccessToken != "tok-1" || creds.RefreshToken != "ref-1" {
		t.Fatalf("creds = %+v", creds)
	}
	if authorizeSeen.Get("resource") != cfg.URL {
		t.Errorf("resource = %q, want %q", authorizeSeen.Get("resource"), cfg.URL)
	}
	if _, ok := src.LoadMCPOAuth("suno"); !ok {
		t.Fatal("токен не сохранён в хранилище")
	}

	// С сохранённым токеном сессия поднимается и инструменты видны.
	sess, err := StartHTTP(t.Context(), cfg, src)
	if err != nil {
		t.Fatalf("StartHTTP с OAuth-токеном: %v", err)
	}
	defer sess.Close()
	if tools := sess.Tools(); len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestAuthorizeServer_StateMismatchRejected(t *testing.T) {
	as := &oauthTestAS{t: t}
	as.start()
	defer as.srv.Close()

	cfg := ServerConfig{ID: "x", Transport: "http", URL: as.srv.URL + "/mcp", TimeoutSec: 30}
	src := &memCreds{}
	open := func(raw string) error {
		u, _ := url.Parse(raw)
		redirect := u.Query().Get("redirect_uri")
		go func() {
			time.Sleep(20 * time.Millisecond)
			resp, err := http.Get(redirect + "?code=code-1&state=wrong")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	if _, err := AuthorizeServer(t.Context(), cfg, src, AuthOptions{OpenURL: open, Timeout: 3 * time.Second}); err == nil {
		t.Fatal("ожидалась ошибка из-за неверного state")
	} else if !strings.Contains(err.Error(), "вход отклонён") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestAuthorizeServer_ManualClientIDWithoutDCR(t *testing.T) {
	as := &oauthTestAS{t: t, noDCR: true}
	as.start()
	defer as.srv.Close()

	cfg := ServerConfig{
		ID: "corp", Label: "corp", Enabled: true, Transport: "http",
		URL: as.srv.URL + "/mcp", TimeoutSec: 30, ClientID: "manual-client",
	}
	src := &memCreds{}
	var seen url.Values
	open := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		seen = u.Query()
		redirect := seen.Get("redirect_uri")
		state := seen.Get("state")
		go func() {
			time.Sleep(20 * time.Millisecond)
			resp, err := http.Get(redirect + "?code=code-1&state=" + url.QueryEscape(state))
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	creds, err := AuthorizeServer(t.Context(), cfg, src, AuthOptions{OpenURL: open, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("AuthorizeServer с ручным client_id: %v", err)
	}
	if got := seen.Get("client_id"); got != "manual-client" {
		t.Fatalf("client_id = %q, want manual-client", got)
	}
	if creds.ClientID != "manual-client" {
		t.Fatalf("сохранённый ClientID = %q", creds.ClientID)
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.registered != 0 {
		t.Fatalf("DCR вызвана %d раз, хотя client_id задан вручную", as.registered)
	}
}

func TestAuthorizeServer_NoDCRAndNoClientID(t *testing.T) {
	as := &oauthTestAS{t: t, noDCR: true}
	as.start()
	defer as.srv.Close()

	cfg := ServerConfig{ID: "corp", Transport: "http", URL: as.srv.URL + "/mcp", TimeoutSec: 30}
	_, err := AuthorizeServer(t.Context(), cfg, &memCreds{}, AuthOptions{Timeout: 3 * time.Second})
	if err == nil {
		t.Fatal("ожидалась ошибка: ни DCR, ни client_id")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

func TestRefreshCredentials(t *testing.T) {
	as := &oauthTestAS{t: t}
	as.start()
	defer as.srv.Close()

	creds, err := RefreshCredentials(t.Context(), "x", Credentials{
		AccessToken: "tok-1", RefreshToken: "ref-1",
		TokenURL: as.srv.URL + "/token", ClientID: "client-1",
	})
	if err != nil {
		t.Fatalf("RefreshCredentials: %v", err)
	}
	if creds.AccessToken != "tok-2" {
		t.Fatalf("access = %q, want tok-2", creds.AccessToken)
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	if as.refreshed != 1 {
		t.Fatalf("refresh вызван %d раз", as.refreshed)
	}
}

func TestCredentialsExpired(t *testing.T) {
	if !(Credentials{}).Expired() {
		t.Fatal("пустой токен должен считаться истёкшим")
	}
	if (Credentials{AccessToken: "x"}).Expired() {
		t.Fatal("без expires_in токен не истекает")
	}
	if !(Credentials{AccessToken: "x", Expiry: time.Now().Add(-time.Minute)}).Expired() {
		t.Fatal("просроченный токен должен истекать")
	}
}
