// Created by nc.ai 1.2.5 on macbook 2026-11-12 20:28:42
// Created by nc.ai 1.2.6 on macbook 2026-11-12
// Package gitrepo создаёт пустой репозиторий на Gitea или GitHub по API.
// Git-операции (init/commit/push) остаются в internal/gitx — здесь только
// «заведи репозиторий на сервере и верни адреса».
package gitrepo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Kind — куда выкладывать новый репозиторий.
type Kind string

const (
	KindNone            Kind = ""
	KindGiteaHome       Kind = "gitea-home"
	KindGiteaKalinamall Kind = "gitea-kalinamall"
	KindGitHub          Kind = "github"
)

// ParseKind понимает то, как пользователь это называет в чате.
func ParseKind(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "none", "no", "нет", "локально", "local":
		return KindNone, nil
	case "gitea", "home", "домашний", "gitea-home", "papatramp":
		return KindGiteaHome, nil
	case "kalinamall", "gitea-kalinamall":
		return KindGiteaKalinamall, nil
	case "github", "гитхаб", "gh":
		return KindGitHub, nil
	}
	return "", fmt.Errorf("неизвестный хостинг %q: поддерживаются github, gitea-home, gitea-kalinamall", s)
}

// Request — параметры создания репозитория.
type Request struct {
	Kind        Kind
	BaseURL     string // для Gitea: https://host[:port]; для GitHub игнорируется
	Token       string
	Owner       string // пусто — личный аккаунт владельца токена
	Name        string
	Description string
	Private     bool
}

// Result — что вернул сервер: адреса для remote и для человека.
type Result struct {
	CloneURL string `json:"clone_url"`
	SSHURL   string `json:"ssh_url"`
	HTMLURL  string `json:"html_url"`
	FullName string `json:"full_name"`
}

// Host — хостинг из настроек (без токена): по нему строим адреса репозитория.
// Токен нужен только для создания по API; сами push идут обычным git на машине
// (SSH-ключи / credential helper), поэтому «заводить входы» в приложении не надо.
type Host struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	BaseURL      string `json:"baseUrl,omitempty"`
	SSHBase      string `json:"sshBase,omitempty"`
	Owner        string `json:"owner,omitempty"`
	PushToCreate bool   `json:"pushToCreate,omitempty"`
}

// KindOf приводит тип хостинга к Kind.
func (h Host) KindOf() Kind {
	if strings.EqualFold(h.Kind, "github") {
		return KindGitHub
	}
	return KindGiteaHome // gitea (адрес берётся из BaseURL)
}

// OwnerOr — владелец по умолчанию, если в запросе не задан.
func (h Host) OwnerOr(owner string) string {
	if o := strings.TrimSpace(owner); o != "" {
		return o
	}
	return strings.TrimSpace(h.Owner)
}

// RepoURLs строит адреса нового репозитория: ssh (предпочтительно — работает по
// уже настроенным ключам) и https.
func (h Host) RepoURLs(owner, name string) (sshURL, httpsURL string) {
	owner = h.OwnerOr(owner)
	name = strings.TrimSpace(name)
	if owner == "" || name == "" {
		return "", ""
	}
	if h.KindOf() == KindGitHub {
		return "git@github.com:" + owner + "/" + name + ".git",
			"https://github.com/" + owner + "/" + name + ".git"
	}
	if base := strings.TrimRight(strings.TrimSpace(h.BaseURL), "/"); base != "" {
		httpsURL = base + "/" + owner + "/" + name + ".git"
	}
	if ssh := strings.TrimSpace(h.SSHBase); ssh != "" {
		if strings.HasPrefix(ssh, "ssh://") {
			sshURL = strings.TrimRight(ssh, "/") + "/" + owner + "/" + name + ".git"
		} else {
			user := "git"
			hostPart := ssh
			if i := strings.Index(hostPart, "@"); i >= 0 {
				user = hostPart[:i]
				hostPart = hostPart[i+1:]
			}
			// С портом scp-форма не работает — нужен ssh://host:port/путь.
			if strings.Contains(hostPart, ":") {
				sshURL = "ssh://" + user + "@" + strings.TrimRight(hostPart, ":") + "/" + owner + "/" + name + ".git"
			} else {
				sshURL = user + "@" + hostPart + ":" + owner + "/" + name + ".git"
			}
		}
	}
	return sshURL, httpsURL
}

// Client создаёт репозитории по HTTP API.
type Client struct {
	HTTP *http.Client
}

// NewClient — клиент с разумным таймаутом (API локального Gitea отвечает быстро,
// но первый TLS до GitHub бывает медленным).
func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Create создаёт пустой репозиторий (auto_init=false: первый коммит будет наш).
func (c *Client) Create(ctx context.Context, req Request) (Result, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return Result{}, fmt.Errorf("пустое имя репозитория")
	}
	if strings.TrimSpace(req.Token) == "" {
		return Result{}, fmt.Errorf("не задан токен для %s — добавьте его в настройках (Settings → Git)", req.Kind)
	}
	switch req.Kind {
	case KindGitHub:
		return c.createGitHub(ctx, req)
	case KindGiteaHome, KindGiteaKalinamall:
		return c.createGitea(ctx, req)
	}
	return Result{}, fmt.Errorf("неизвестный хостинг %q", req.Kind)
}

// createGitea создаёт репозиторий на Gitea. BaseURL — адрес сервера.
func (c *Client) createGitea(ctx context.Context, req Request) (Result, error) {
	base := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if base == "" {
		return Result{}, fmt.Errorf("не задан адрес Gitea-сервера (поле baseUrl у хостинга)")
	}
	path := base + "/api/v1/user/repos"
	if owner := strings.TrimSpace(req.Owner); owner != "" {
		path = base + "/api/v1/orgs/" + owner + "/repos"
	}
	body, _ := json.Marshal(map[string]any{
		"name":        req.Name,
		"private":     req.Private,
		"auto_init":   false,
		"description": strings.TrimSpace(req.Description),
	})
	raw, err := c.do(ctx, http.MethodPost, path, req.Token, body)
	if err != nil {
		return Result{}, err
	}
	var out Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, fmt.Errorf("ответ Gitea не JSON: %w", err)
	}
	if out.SSHURL == "" && out.CloneURL == "" {
		return Result{}, fmt.Errorf("Gitea не вернул адрес репозитория")
	}
	return out, nil
}

func (c *Client) createGitHub(ctx context.Context, req Request) (Result, error) {
	path := "https://api.github.com/user/repos"
	if owner := strings.TrimSpace(req.Owner); owner != "" {
		path = "https://api.github.com/orgs/" + owner + "/repos"
	}
	body, _ := json.Marshal(map[string]any{
		"name":        req.Name,
		"private":     req.Private,
		"description": strings.TrimSpace(req.Description),
		"auto_init":   false,
	})
	raw, err := c.do(ctx, http.MethodPost, path, req.Token, body)
	if err != nil {
		return Result{}, err
	}
	var out Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return Result{}, fmt.Errorf("ответ GitHub не JSON: %w", err)
	}
	if out.CloneURL == "" && out.SSHURL == "" {
		return Result{}, fmt.Errorf("GitHub не вернул адрес репозитория")
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, method, url, token string, body []byte) ([]byte, error) {
	httpc := c.HTTP
	if httpc == nil {
		httpc = NewClient().HTTP
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "token "+token)
	if strings.Contains(url, "api.github.com") {
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode/100 == 2:
		return raw, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("%s: токен отклонён (401) — проверьте настройки", url)
	case resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusUnprocessableEntity:
		return nil, fmt.Errorf("репозиторий уже существует (%d): %s", resp.StatusCode, firstMessage(raw))
	case resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("нет прав на создание репозитория (403): %s", firstMessage(raw))
	}
	return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, firstMessage(raw))
}

// firstMessage достаёт человекочитаемое сообщение из тела ошибки API.
func firstMessage(raw []byte) string {
	var payload struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		if m := strings.TrimSpace(payload.Message); m != "" {
			if len(payload.Errors) > 0 && payload.Errors[0].Message != "" {
				return m + ": " + payload.Errors[0].Message
			}
			return m
		}
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
