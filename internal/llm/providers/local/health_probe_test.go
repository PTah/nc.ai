package local

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// LM Studio, Lemonade и vLLM не имеют Ollama-эндпоинтов: раньше проба каждый
// раз дёргала /api/ps и /api/generate, получала 404 и только потом пинговала чат.
func TestProbeHealthNonOllamaServer(t *testing.T) {
	var ollamaCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps", "/api/generate", "/api/api/ps", "/api/api/generate":
			ollamaCalls++
			http.NotFound(w, r)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "m")
	c.http = srv.Client()

	rep := c.ProbeHealth(context.Background())
	if !c.isKnownNotOllama() {
		t.Fatalf("server must be detected as non-Ollama: %+v", rep)
	}
	if rep.Label == "Local down" {
		t.Fatalf("healthy OpenAI-compatible server must not be down: %+v", rep)
	}

	callsAfterFirst := ollamaCalls
	_ = c.ProbeHealth(context.Background())
	if ollamaCalls != callsAfterFirst {
		t.Fatalf("ollama endpoints called again: %d -> %d", callsAfterFirst, ollamaCalls)
	}
}

func TestProbeHealthOllamaServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"m","size_vram":8000000000}]}`))
		case "/api/generate":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":"ok","eval_count":1,"eval_duration":1000000}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "m")
	c.http = srv.Client()
	rep := c.ProbeHealth(context.Background())
	if c.isKnownNotOllama() {
		t.Fatalf("ollama server must not be marked non-Ollama: %+v", rep)
	}
	if !rep.VRAMLoaded || rep.SizeVRAM == 0 {
		t.Fatalf("vram info expected: %+v", rep)
	}
}
