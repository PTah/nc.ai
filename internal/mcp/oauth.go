// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:37:14
// Edited by nc.ai 1.1.17 on new-admin-pc 2026-11-12 09:48:47
// Created by nc.ai 1.1.16 on macbook 2026-11-11
package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Credentials — то, что мы храним в секретах (keychain / Credential Manager)
// для сервера с OAuth. В settings.json этого быть не должно.
type Credentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	TokenURL     string    `json:"token_endpoint,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	Issuer       string    `json:"issuer,omitempty"`
}

// Expired сообщает, пора ли обновлять токен (запас 60 с на сетевые задержки).
func (c Credentials) Expired() bool {
	if c.AccessToken == "" {
		return true
	}
	if c.Expiry.IsZero() {
		return false // сервер не выдал expires_in — считаем бессрочным
	}
	return time.Now().After(c.Expiry.Add(-60 * time.Second))
}

// CredentialSource — хранилище токенов (реализуется в app поверх internal/config).
type CredentialSource interface {
	LoadMCPOAuth(serverID string) (Credentials, bool)
	SaveMCPOAuth(serverID string, c Credentials) error
	ClearMCPOAuth(serverID string) error
}

// AuthOptions — зависимости интерактивного входа, чтобы пакет не зависел от UI.
type AuthOptions struct {
	// OpenURL открывает браузер (в приложении — Wails runtime).
	OpenURL func(url string) error
	// Timeout ожидания кода авторизации; по умолчанию 5 минут.
	Timeout time.Duration
}

type asMetadata struct {
	Issuer                      string   `json:"issuer"`
	AuthorizationEndpoint       string   `json:"authorization_endpoint"`
	TokenEndpoint               string   `json:"token_endpoint"`
	RegistrationEndpoint        string   `json:"registration_endpoint"`
	ScopesSupported             []string `json:"scopes_supported"`
	CodeChallengeMethodsSupport []string `json:"code_challenge_methods_supported"`
}

// AuthorizeServer выполняет OAuth-вход для MCP-сервера: discovery → dynamic
// client registration (если есть) → PKCE → локальный редирект → обмен кода на
// токен. Найденные токены складываются через src, в настройки не попадают.
func AuthorizeServer(ctx context.Context, cfg ServerConfig, src CredentialSource, opt AuthOptions) (Credentials, error) {
	if opt.Timeout <= 0 {
		opt.Timeout = 5 * time.Minute
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return Credentials{}, err
	}
	base, err := url.Parse(cfg.URL)
	if err != nil {
		return Credentials{}, fmt.Errorf("mcp %s: разбор URL: %w", cfg.ID, err)
	}
	meta, err := discoverAuthServer(ctx, base)
	if err != nil {
		return Credentials{}, err
	}
	if meta.TokenEndpoint == "" || meta.AuthorizationEndpoint == "" {
		return Credentials{}, fmt.Errorf("mcp %s: сервер не отдал OAuth-эндпоинты (authorization/token)", cfg.ID)
	}
	if len(meta.CodeChallengeMethodsSupport) > 0 && !containsFold(meta.CodeChallengeMethodsSupport, "S256") {
		return Credentials{}, fmt.Errorf("mcp %s: сервер не поддерживает PKCE S256 — вход отменён", cfg.ID)
	}

	// Локальный слушатель для редиректа. Слушаем только loopback.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Credentials{}, fmt.Errorf("mcp %s: не удалось открыть локальный порт для редиректа: %w", cfg.ID, err)
	}
	defer ln.Close()
	redirectURI := fmt.Sprintf("http://%s/callback", ln.Addr().String())

	clientID, scope := "", ""
	prev, havePrev := src.LoadMCPOAuth(cfg.ID)
	if havePrev {
		clientID = prev.ClientID
		scope = prev.Scope
	}
	// Явный client_id из карточки сервера главнее всего: провайдеры без DCR
	// (Suno, корпоративные шлюзы) выдают его вручную.
	if id := strings.TrimSpace(cfg.ClientID); id != "" {
		clientID = id
	}
	if meta.RegistrationEndpoint != "" && clientID == "" {
		clientID, err = registerClient(ctx, meta.RegistrationEndpoint, redirectURI)
		if err != nil {
			return Credentials{}, fmt.Errorf("mcp %s: регистрация клиента: %w", cfg.ID, err)
		}
	}
	if clientID == "" {
		return Credentials{}, fmt.Errorf("mcp %s: сервер не поддерживает автоматическую регистрацию клиента — "+
			"впишите client_id провайдера в карточку сервера", cfg.ID)
	}

	verifier, err := randomToken(32)
	if err != nil {
		return Credentials{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state, err := randomToken(16)
	if err != nil {
		return Credentials{}, err
	}

	authURL, _ := url.Parse(meta.AuthorizationEndpoint)
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	// RFC 8707: просим токен именно под этот MCP-ресурс, если сервер понимает.
	q.Set("resource", cfg.URL)
	if scope != "" {
		q.Set("scope", scope)
	} else if len(meta.ScopesSupported) == 1 {
		q.Set("scope", meta.ScopesSupported[0])
	}
	authURL.RawQuery = q.Encode()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			desc := q.Get("error_description")
			http.Error(w, "Ошибка авторизации: "+e+" "+desc, http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("%s %s", e, desc):
			default:
			}
			return
		}
		if q.Get("state") != state {
			http.Error(w, "Неверный state — вход отклонён", http.StatusBadRequest)
			select {
			case errCh <- fmt.Errorf("state mismatch"):
			default:
			}
			return
		}
		if c := q.Get("code"); c != "" {
			select {
			case codeCh <- c:
			default:
			}
			_, _ = io.WriteString(w, "<html><body>Готово. Вернитесь в NotCursor.</body></html>")
			return
		}
		http.Error(w, "Нет кода авторизации", http.StatusBadRequest)
	})
	srv.Handler = mux
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	if opt.OpenURL == nil {
		return Credentials{}, fmt.Errorf("mcp %s: не задан способ открыть браузер", cfg.ID)
	}
	if err := opt.OpenURL(authURL.String()); err != nil {
		return Credentials{}, fmt.Errorf("mcp %s: не удалось открыть браузер: %w", cfg.ID, err)
	}

	timer := time.NewTimer(opt.Timeout)
	defer timer.Stop()
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return Credentials{}, fmt.Errorf("mcp %s: вход отклонён: %w", cfg.ID, err)
	case <-timer.C:
		return Credentials{}, fmt.Errorf("mcp %s: вход не завершён за %s — окно авторизации закрыто?", cfg.ID,
			humanShort(opt.Timeout))
	case <-ctx.Done():
		return Credentials{}, ctx.Err()
	}

	creds, err := exchangeCode(ctx, meta.TokenEndpoint, clientID, redirectURI, code, verifier)
	if err != nil {
		return Credentials{}, fmt.Errorf("mcp %s: обмен кода на токен: %w", cfg.ID, err)
	}
	creds.ClientID = clientID
	creds.TokenURL = meta.TokenEndpoint
	creds.Issuer = meta.Issuer
	if scope := authURL.Query().Get("scope"); scope != "" {
		creds.Scope = scope
	}
	if err := src.SaveMCPOAuth(cfg.ID, creds); err != nil {
		return creds, fmt.Errorf("mcp %s: токен получен, но не сохранён в секреты: %w", cfg.ID, err)
	}
	return creds, nil
}

// RefreshCredentials обновляет access-токен по refresh_token.
func RefreshCredentials(ctx context.Context, serverID string, c Credentials) (Credentials, error) {
	if c.RefreshToken == "" || c.TokenURL == "" {
		return c, fmt.Errorf("mcp %s: нет refresh_token — нужен повторный вход", serverID)
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", c.RefreshToken)
	if c.ClientID != "" {
		form.Set("client_id", c.ClientID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return c, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return c, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return c, fmt.Errorf("HTTP %d при обновлении токена: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return c, fmt.Errorf("ответ token endpoint не JSON: %w", err)
	}
	if tok.AccessToken == "" {
		return c, fmt.Errorf("token endpoint не вернул access_token")
	}
	out := c
	out.AccessToken = tok.AccessToken
	out.TokenType = tok.TokenType
	if tok.RefreshToken != "" {
		out.RefreshToken = tok.RefreshToken
	}
	if tok.ExpiresIn > 0 {
		out.Expiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return out, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
}

func exchangeCode(ctx context.Context, tokenURL, clientID, redirectURI, code, verifier string) (Credentials, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Credentials{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return Credentials{}, fmt.Errorf("ответ token endpoint не JSON: %w", err)
	}
	if tok.AccessToken == "" {
		return Credentials{}, fmt.Errorf("token endpoint не вернул access_token")
	}
	c := Credentials{AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, TokenType: tok.TokenType}
	if tok.ExpiresIn > 0 {
		c.Expiry = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	return c, nil
}

// discoverAuthServer ищет OAuth-метаданные: сначала protected-resource (RFC 9728),
// потом authorization-server metadata (RFC 8414); если ничего нет — предполагаем,
// что MCP-сервер сам является авторизационным сервером.
func discoverAuthServer(ctx context.Context, mcpURL *url.URL) (asMetadata, error) {
	origin := mcpURL.Scheme + "://" + mcpURL.Host
	asURL := origin
	var pr struct {
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := getJSON(ctx, origin+"/.well-known/oauth-protected-resource", &pr); err == nil && len(pr.AuthorizationServers) > 0 {
		asURL = pr.AuthorizationServers[0]
	}
	metaURL := strings.TrimRight(asURL, "/") + "/.well-known/oauth-authorization-server"
	var meta asMetadata
	if err := getJSON(ctx, metaURL, &meta); err != nil {
		var oidc asMetadata
		if err2 := getJSON(ctx, strings.TrimRight(asURL, "/")+"/.well-known/openid-configuration", &oidc); err2 != nil {
			return asMetadata{}, fmt.Errorf("OAuth-метаданные не найдены (%s): %w", metaURL, err)
		}
		meta = oidc
	}
	if meta.Issuer == "" {
		meta.Issuer = asURL
	}
	return meta, nil
}

func registerClient(ctx context.Context, endpoint, redirectURI string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"client_name":                "NotCursor.ai",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"application_type":           "native",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(raw, &reg); err != nil {
		return "", fmt.Errorf("ответ регистрации не JSON: %w", err)
	}
	if reg.ClientID == "" {
		return "", fmt.Errorf("сервер не вернул client_id")
	}
	return reg.ClientID, nil
}

func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}

func humanShort(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0f с", d.Seconds())
	}
	return fmt.Sprintf("%.0f мин", d.Minutes())
}
