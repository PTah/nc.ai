// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:27:52
// Created by nc.ai 1.1.16 on macbook 2026-11-11
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"notcursor.ai/app/internal/netx"
)

// httpSession — Streamable HTTP сессия MCP-сервера: JSON-RPC поверх POST на один
// эндпоинт. Ответ приходит либо как application/json (один объект), либо как
// text/event-stream (SSE, один или несколько объектов).
//
// OAuth здесь нет: сервер должен принимать статический заголовок (Bearer-токен).
// Интерактивный вход (discovery + PKCE) — следующий шаг роадмапа §2a-2.
type httpSession struct {
	cfg    ServerConfig
	client *http.Client
	url    string
	// sessionID — Mcp-Session-Id, который выдаёт сервер; обязателен в следующих
	// запросах, иначе сервер считает их новым подключением.
	sessionID string

	mu     sync.Mutex // сериализуем запросы: сессия под одним id
	nextID atomic.Uint64
	closed atomic.Bool
	dead   atomic.Bool

	// src/creds — OAuth: токен берём из секретов и обновляем по refresh_token.
	src   CredentialSource
	creds Credentials

	logMu sync.Mutex
	logs  []string

	tools []mcpTool
}

// httpMaxLogs — сколько последних строк держим для панели статуса.
const httpMaxLogs = 200

// StartHTTP проверяет адрес, делает initialize + tools/list и возвращает живую
// сессию. Конфиг должен быть нормализован (Normalize вызывается здесь же).
func StartHTTP(ctx context.Context, cfg ServerConfig, src CredentialSource) (*httpSession, error) {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	s := &httpSession{
		cfg: cfg,
		url: cfg.URL,
		src: src,
		client: &http.Client{
			Timeout: 0, // таймауты — по контексту запроса
			// Редирект на другой хост увёл бы токен к чужому серверу.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
					return fmt.Errorf("mcp %s: отказ от редиректа на %s", cfg.ID, req.URL.Host)
				}
				if len(via) >= 5 {
					return fmt.Errorf("mcp %s: слишком много редиректов", cfg.ID)
				}
				return nil
			},
		},
	}
	boot := time.Duration(cfg.TimeoutSec) * time.Second
	if boot < 30*time.Second {
		boot = 30 * time.Second
	}
	initCtx, cancel := context.WithTimeout(ctx, boot)
	defer cancel()
	if err := s.prepareAuth(initCtx); err != nil {
		s.Close()
		return nil, err
	}
	if err := s.initialize(initCtx); err != nil {
		s.Close()
		return nil, err
	}
	listCtx, cancelList := context.WithTimeout(ctx, boot)
	defer cancelList()
	if err := s.refreshTools(listCtx); err != nil {
		s.Close()
		return nil, err
	}
	s.logf("started url=%s tools=%d", s.url, len(s.tools))
	return s, nil
}

// prepareAuth подхватывает сохранённый OAuth-токен и обновляет его, если истёк.
func (s *httpSession) prepareAuth(ctx context.Context) error {
	if s.src == nil || s.hasStaticAuth() {
		return nil
	}
	if c, ok := s.src.LoadMCPOAuth(s.cfg.ID); ok {
		s.creds = c
	}
	if s.creds.AccessToken == "" {
		return nil // вход не выполнялся: сервер может быть анонимным
	}
	if !s.creds.Expired() {
		return nil
	}
	refreshed, err := RefreshCredentials(ctx, s.cfg.ID, s.creds)
	if err != nil {
		// Не валим сессию: если сервер пускает по старому токену — работаем,
		// иначе 401 даст понятную ошибку «нужен вход».
		s.logf("oauth refresh failed: %v", err)
		return nil
	}
	s.creds = refreshed
	if err := s.src.SaveMCPOAuth(s.cfg.ID, refreshed); err != nil {
		s.logf("oauth save failed: %v", err)
	}
	s.logf("oauth token refreshed")
	return nil
}

func (s *httpSession) hasStaticAuth() bool {
	for k := range s.cfg.Headers {
		if strings.EqualFold(k, "Authorization") {
			return true
		}
	}
	return false
}

// refreshLocked обновляет токен по refresh_token (вызывается под mu).
func (s *httpSession) refreshLocked(ctx context.Context) bool {
	if s.src == nil || s.creds.RefreshToken == "" {
		return false
	}
	refreshed, err := RefreshCredentials(ctx, s.cfg.ID, s.creds)
	if err != nil {
		s.logf("oauth refresh failed: %v", err)
		return false
	}
	s.creds = refreshed
	if err := s.src.SaveMCPOAuth(s.cfg.ID, refreshed); err != nil {
		s.logf("oauth save failed: %v", err)
	}
	s.logf("oauth token refreshed on 401")
	return true
}

func (s *httpSession) initialize(ctx context.Context) error {
	params := initializeParams{
		ProtocolVersion: protocolVersion,
		Capabilities:    map[string]any{},
		ClientInfo: map[string]any{
			"name":    "NotCursor.ai",
			"version": "mcp-http",
		},
	}
	if _, err := s.request(ctx, "initialize", params); err != nil {
		return fmt.Errorf("mcp %s: initialize: %w", s.cfg.ID, err)
	}
	// По протоколу после initialize клиент шлёт notifications/initialized.
	// Ответ на уведомление не обязателен: 202 Accepted — норма.
	_ = s.notify(ctx, "notifications/initialized", map[string]any{})
	return nil
}

func (s *httpSession) refreshTools(ctx context.Context) error {
	raw, err := s.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return fmt.Errorf("mcp %s: tools/list: %w", s.cfg.ID, err)
	}
	var res toolListResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("mcp %s: tools/list json: %w", s.cfg.ID, err)
	}
	s.mu.Lock()
	s.tools = res.Tools
	s.mu.Unlock()
	return nil
}

func (s *httpSession) Tools() []mcpTool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]mcpTool, len(s.tools))
	copy(out, s.tools)
	return out
}

func (s *httpSession) Alive() bool {
	return s != nil && !s.closed.Load() && !s.dead.Load()
}

func (s *httpSession) Logs() []string {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	out := make([]string, len(s.logs))
	copy(out, s.logs)
	return out
}

func (s *httpSession) Close() {
	if s == nil || !s.closed.CompareAndSwap(false, true) {
		return
	}
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
	s.logf("closed")
}

// CallTool вызывает tools/call и склеивает текстовые части ответа.
func (s *httpSession) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	timeout := time.Duration(s.cfg.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := s.request(cctx, "tools/call", callToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", fmt.Errorf("mcp %s/%s: %w", s.cfg.ID, name, err)
	}
	var res callToolResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("mcp %s/%s: ответ не JSON: %w", s.cfg.ID, name, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(c.Text)
	}
	out := b.String()
	if res.IsError {
		if out == "" {
			out = "инструмент вернул isError без текста"
		}
		return "", fmt.Errorf("%s", out)
	}
	return out, nil
}

// request отправляет JSON-RPC запрос с id и ждёт ответ с тем же id.
func (s *httpSession) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return nil, fmt.Errorf("mcp session closed")
	}
	id := s.nextID.Add(1)
	body, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	resp, err := s.post(ctx, body)
	if err != nil {
		return nil, err
	}
	// 401 при живом refresh_token — это истёкший access: обновляем и повторяем
	// ровно один раз, чтобы не зациклиться на сервере, который всегда 401.
	if resp.StatusCode == http.StatusUnauthorized && s.refreshLocked(ctx) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		resp, err = s.post(ctx, body)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		if s.creds.AccessToken == "" && !s.hasStaticAuth() {
			return nil, fmt.Errorf("сервер требует входа (HTTP %d): нажмите «Войти» в Settings → MCP "+
				"или задайте токен в заголовках", resp.StatusCode)
		}
		return nil, fmt.Errorf("сервер отклонил токен (HTTP %d) — войдите заново в Settings → MCP", resp.StatusCode)
	case http.StatusNotFound:
		return nil, fmt.Errorf("эндпоинт не найден (HTTP 404) — проверьте URL %s", s.url)
	case http.StatusAccepted:
		return nil, fmt.Errorf("сервер принял запрос без ответа (HTTP 202) для %s", method)
	default:
		return nil, fmt.Errorf("HTTP %d на %s", resp.StatusCode, method)
	}

	msgs, err := readJSONRPC(resp)
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		if !sameID(m.ID, id) {
			continue
		}
		if m.Error != nil {
			return nil, m.Error
		}
		return m.Result, nil
	}
	return nil, fmt.Errorf("нет ответа на %s (id=%d) в потоке", method, id)
}

// notify отправляет уведомление (без id). Ответ сервера не разбираем: по
// протоколу это 202, но часть серверов отвечает 200 с пустым телом.
func (s *httpSession) notify(ctx context.Context, method string, params any) error {
	body, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return err
	}
	resp, err := s.post(ctx, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("HTTP %d на %s", resp.StatusCode, method)
	}
	return nil
}

func (s *httpSession) post(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
	for k, v := range s.cfg.Headers {
		if strings.TrimSpace(k) == "" {
			continue
		}
		req.Header.Set(k, v)
	}
	if s.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", s.sessionID)
	}
	if !s.hasStaticAuth() && s.creds.AccessToken != "" {
		typ := s.creds.TokenType
		if strings.TrimSpace(typ) == "" {
			typ = "Bearer"
		}
		req.Header.Set("Authorization", typ+" "+s.creds.AccessToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		s.sessionID = sid
	}
	return resp, nil
}

// readJSONRPC читает тело ответа: либо один JSON-объект, либо SSE-поток с
// объектами JSON-RPC (сервер может прислать несколько — уведомления и ответ).
func readJSONRPC(resp *http.Response) ([]jsonRPCResponse, error) {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "text/event-stream") {
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(data)) == 0 {
			return nil, nil
		}
		var one jsonRPCResponse
		if err := json.Unmarshal(data, &one); err == nil {
			return []jsonRPCResponse{one}, nil
		}
		var many []jsonRPCResponse
		if err := json.Unmarshal(data, &many); err != nil {
			return nil, fmt.Errorf("ответ не JSON-RPC: %w", err)
		}
		return many, nil
	}

	var out []jsonRPCResponse
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var data strings.Builder
	flush := func() {
		chunk := strings.TrimSpace(data.String())
		data.Reset()
		if chunk == "" || chunk == "[DONE]" {
			return
		}
		var m jsonRPCResponse
		if err := json.Unmarshal([]byte(chunk), &m); err == nil {
			out = append(out, m)
		}
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, ":"):
			// комментарий/keepalive
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteString("\n")
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("чтение SSE: %w", err)
	}
	return out, nil
}

// sameID сравнивает id из ответа с нашим: JSON-число приходит как float64.
func sameID(got any, want uint64) bool {
	switch v := got.(type) {
	case float64:
		return uint64(v) == want
	case string:
		return v == fmt.Sprintf("%d", want)
	default:
		return false
	}
}

func (s *httpSession) logf(format string, args ...any) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if len(s.logs) >= httpMaxLogs {
		copy(s.logs, s.logs[len(s.logs)-httpMaxLogs/2:])
		s.logs = s.logs[:httpMaxLogs/2]
	}
	s.logs = append(s.logs, fmt.Sprintf(format, args...))
}

// Validate проверяет адрес hosted-сервера: https (кроме localhost), без
// логина/пароля в URL, без внутренних адресов — если пользователь явно не
// разрешил их. Опирается на общую SSRF-проверку `internal/netx`, но с одной
// разницей: localhost разрешён (типичный локальный шлюз MCP на 127.0.0.1).
func (c *ServerConfig) Validate() error {
	if c.Transport != "" && c.Transport != "http" {
		return nil // stdio проверяется отдельно
	}
	raw := strings.TrimSpace(c.URL)
	if raw == "" {
		return fmt.Errorf("mcp %s: пустой URL для transport=http", c.ID)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("mcp %s: разбор URL: %w", c.ID, err)
	}
	if u.User != nil {
		return fmt.Errorf("mcp %s: логин/пароль в URL не поддерживаются — используйте заголовок Authorization", c.ID)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("mcp %s: в URL нет хоста", c.ID)
	}
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !loopback && !c.AllowPrivate {
			return fmt.Errorf("mcp %s: http разрешён только для localhost — для внешнего сервера нужен https", c.ID)
		}
	default:
		return fmt.Errorf("mcp %s: поддерживаются только http/https, получено %q", c.ID, u.Scheme)
	}
	if c.AllowPrivate {
		return nil
	}
	if loopback {
		return nil
	}
	if _, err := netx.CheckURL(raw); err != nil {
		return fmt.Errorf("mcp %s: %w (включите AllowPrivate, если это ваш сервер)", c.ID, err)
	}
	return nil
}
