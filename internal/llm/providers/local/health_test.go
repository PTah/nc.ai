package local

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestScoreHealth(t *testing.T) {
	ok := HealthReport{PingMs: 400, TokPerSec: 80, VRAMLoaded: true, SizeVRAM: 5 << 30}
	scoreHealth(&ok)
	if ok.Level != HealthOK {
		t.Fatalf("ok: %s", ok.Level)
	}
	warn := HealthReport{PingMs: 2000, TokPerSec: 20, VRAMLoaded: false, LoadMs: 5000}
	scoreHealth(&warn)
	if warn.Level != HealthWarn {
		t.Fatalf("warn: %s", warn.Level)
	}
	bad := HealthReport{PingMs: 6000, TokPerSec: 5}
	scoreHealth(&bad)
	if bad.Level != HealthBad {
		t.Fatalf("bad: %s", bad.Level)
	}
	crit := HealthReport{PingMs: 20000}
	scoreHealth(&crit)
	if crit.Level != HealthCritical {
		t.Fatalf("crit: %s", crit.Level)
	}
}

func TestProbeHealthOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/ps":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{
					"name": "qwen2.5-coder:7b", "size_vram": 4987132312,
				}},
			})
		case r.URL.Path == "/api/generate":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"response": "ok", "total_duration": 4e8, "load_duration": 1e6,
				"eval_count": 2, "eval_duration": 2e7,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := New(srv.URL+"/v1", "", "qwen2.5-coder:7b")
	c.http = srv.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rep := c.ProbeHealth(ctx)
	if rep.Level != HealthOK {
		t.Fatalf("level=%s err=%s detail=%s", rep.Level, rep.Error, rep.Detail)
	}
	if !rep.VRAMLoaded || rep.TokPerSec < 50 {
		t.Fatalf("%+v", rep)
	}
	if !strings.Contains(rep.Label, "tok/s") {
		t.Fatalf("label=%q", rep.Label)
	}
}
