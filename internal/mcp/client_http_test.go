// Created by nc.ai 1.1.15 on macbook 2026-11-11 21:29:21
// Created by nc.ai 1.1.16 on macbook 2026-11-11
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// mcpTestServer — минимальный Streamable HTTP MCP-сервер: initialize,
// notifications/initialized (202), tools/list и tools/call. Отдаёт либо JSON,
// либо SSE (по флагу), и запоминает Mcp-Session-Id из заголовков.
type mcpTestServer struct {
	t      *testing.T
	sse    bool
	mu     sync.Mutex
	gotSID []string
	calls  int
}

func (s *mcpTestServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Errorf("bad json body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		s.gotSID = append(s.gotSID, r.Header.Get("Mcp-Session-Id"))
		s.calls++
		s.mu.Unlock()
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			s.t.Errorf("Authorization header = %q, want Bearer test-token", got)
		}

		method, _ := body["method"].(string)
		id := body["id"]
		switch method {
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
			return
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-42")
			s.reply(w, id, map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "test"},
			})
		case "tools/list":
			s.reply(w, id, map[string]any{
				"tools": []map[string]any{{
					"name":        "echo",
					"description": "echo back",
					"inputSchema": map[string]any{"type": "object"},
				}},
			})
		case "tools/call":
			s.reply(w, id, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "pong"}},
			})
		default:
			s.t.Errorf("unexpected method %q", method)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (s *mcpTestServer) reply(w http.ResponseWriter, id any, result map[string]any) {
	payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if s.sse {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, ": keepalive\n\ndata: %s\n\n", payload)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func testHTTPConfig(url string) ServerConfig {
	return ServerConfig{
		ID:         "remote",
		Label:      "Remote",
		Enabled:    true,
		Transport:  "http",
		URL:        url,
		Headers:    map[string]string{"Authorization": "Bearer test-token"},
		TimeoutSec: 30,
	}
}

func TestStartHTTP_JSONTransport(t *testing.T) {
	srv := &mcpTestServer{t: t}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	sess, err := StartHTTP(t.Context(), testHTTPConfig(ts.URL+"/mcp"), nil)
	if err != nil {
		t.Fatalf("StartHTTP: %v", err)
	}
	defer sess.Close()

	tools := sess.Tools()
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v, want echo", tools)
	}
	out, err := sess.CallTool(t.Context(), "echo", map[string]any{"text": "ping"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "pong" {
		t.Fatalf("CallTool = %q, want pong", out)
	}
	if !sess.Alive() {
		t.Fatal("сессия должна быть живой")
	}

	// Первый запрос идёт без id сессии, дальше — с выданным сервером.
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.gotSID) < 2 {
		t.Fatalf("мало запросов: %v", srv.gotSID)
	}
	if srv.gotSID[0] != "" {
		t.Fatalf("первый запрос должен быть без Mcp-Session-Id, получено %q", srv.gotSID[0])
	}
	last := srv.gotSID[len(srv.gotSID)-1]
	if last != "sess-42" {
		t.Fatalf("Mcp-Session-Id не переиспользован: %q", last)
	}
}

func TestStartHTTP_SSEResponse(t *testing.T) {
	srv := &mcpTestServer{t: t, sse: true}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	sess, err := StartHTTP(t.Context(), testHTTPConfig(ts.URL), nil)
	if err != nil {
		t.Fatalf("StartHTTP over SSE: %v", err)
	}
	defer sess.Close()
	if got := len(sess.Tools()); got != 1 {
		t.Fatalf("tools = %d, want 1", got)
	}
}

func TestStartHTTP_UnauthorizedMentionsAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	_, err := StartHTTP(t.Context(), testHTTPConfig(ts.URL), nil)
	if err == nil {
		t.Fatal("ожидалась ошибка авторизации")
	}
	if !strings.Contains(err.Error(), "войдите заново") && !strings.Contains(err.Error(), "требует входа") {
		t.Fatalf("в ошибке нет подсказки про вход: %v", err)
	}
}

func TestStartHTTP_RespectsTimeout(t *testing.T) {
	done := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}))
	defer ts.Close()
	defer close(done)

	cfg := testHTTPConfig(ts.URL)
	cfg.TimeoutSec = 1 // но минимум для bootstrap — 30 с, поэтому проверяем отмену контекста
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if _, err := StartHTTP(ctx, cfg, nil); err == nil {
		t.Fatal("ожидалась ошибка по таймауту контекста")
	}
}

func TestServerConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     ServerConfig
		wantErr string
	}{
		{
			name: "https ok",
			cfg:  ServerConfig{ID: "x", Transport: "http", URL: "https://api.suno.com/mcp"},
		},
		{
			name:    "http внешний хост",
			cfg:     ServerConfig{ID: "x", Transport: "http", URL: "http://api.example.com/mcp"},
			wantErr: "только для localhost",
		},
		{
			name: "http localhost ok",
			cfg:  ServerConfig{ID: "x", Transport: "http", URL: "http://localhost:8000/mcp"},
		},
		{
			name:    "RFC1918 без разрешения",
			cfg:     ServerConfig{ID: "x", Transport: "http", URL: "https://192.168.128.48:3000/mcp"},
			wantErr: "blocked IP",
		},
		{
			name: "RFC1918 с AllowPrivate",
			cfg:  ServerConfig{ID: "x", Transport: "http", URL: "https://192.168.128.48:3000/mcp", AllowPrivate: true},
		},
		{
			name:    "креды в URL",
			cfg:     ServerConfig{ID: "x", Transport: "http", URL: "https://user:pass@example.com/mcp"},
			wantErr: "логин/пароль",
		},
		{
			name:    "пустой URL",
			cfg:     ServerConfig{ID: "x", Transport: "http"},
			wantErr: "пустой URL",
		},
		{
			name:    "не http схема",
			cfg:     ServerConfig{ID: "x", Transport: "http", URL: "ftp://example.com/mcp"},
			wantErr: "только http/https",
		},
	}
	for _, c := range cases {
		err := c.cfg.Validate()
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("%s: неожиданная ошибка: %v", c.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: ожидалась ошибка, получено nil", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s: ошибка %q не содержит %q", c.name, err.Error(), c.wantErr)
		}
	}
}

// Конфиг без transport, но с URL — это hosted-сервер, и он должен сам стать http.
func TestServerConfigNormalizeInfersHTTP(t *testing.T) {
	cfg := ServerConfig{ID: "x", URL: "https://api.example.com/mcp"}
	cfg.Normalize()
	if cfg.Transport != "http" {
		t.Fatalf("transport = %q, want http", cfg.Transport)
	}
	stdio := ServerConfig{ID: "y", Command: "uvx"}
	stdio.Normalize()
	if stdio.Transport != "stdio" {
		t.Fatalf("transport = %q, want stdio", stdio.Transport)
	}
}
