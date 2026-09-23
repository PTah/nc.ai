package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"notcursor.ai/app/internal/llm"
)

// HealthLevel is a traffic-light style status for the UI.
type HealthLevel string

const (
	HealthOK       HealthLevel = "ok"       // green
	HealthWarn     HealthLevel = "warn"     // yellow
	HealthBad      HealthLevel = "bad"      // red
	HealthCritical HealthLevel = "critical" // burgundy
)

// HealthReport is a quick Local/Ollama readiness snapshot.
type HealthReport struct {
	Level           HealthLevel `json:"level"`
	Label           string      `json:"label"`
	Detail          string      `json:"detail"`
	Model           string      `json:"model"`
	PingMs          int         `json:"pingMs"`
	TokPerSec       float64     `json:"tokPerSec"` // alias of DecodeTokPerSec for UI
	PrefillMs       int         `json:"prefillMs"`
	DecodeTokPerSec float64     `json:"decodeTokPerSec"`
	VRAMLoaded      bool        `json:"vramLoaded"`
	SizeVRAM        int64       `json:"sizeVram"`
	LoadMs          int         `json:"loadMs"`
	Hint            string      `json:"hint,omitempty"`
	Error           string      `json:"error,omitempty"`
	// Kind — определённый тип сервера: "ollama" или "openai" (Lemonade, LM Studio,
	// vLLM). Пусто, пока не определили.
	Kind string `json:"kind,omitempty"`
}

type ollamaPSResponse struct {
	Models []struct {
		Name     string `json:"name"`
		Model    string `json:"model"`
		SizeVRAM int64  `json:"size_vram"`
	} `json:"models"`
}

type ollamaGenerateResponse struct {
	TotalDuration      int64 `json:"total_duration"`
	LoadDuration       int64 `json:"load_duration"`
	PromptEvalCount    int   `json:"prompt_eval_count"`
	PromptEvalDuration int64 `json:"prompt_eval_duration"`
	EvalCount          int   `json:"eval_count"`
	EvalDuration       int64 `json:"eval_duration"`
}

// ProbeHealth checks VRAM residency (Ollama /api/ps) and a tiny generate/chat ping.
func (c *Client) ProbeHealth(ctx context.Context) HealthReport {
	model := strings.TrimSpace(c.model)
	rep := HealthReport{Model: model, Level: HealthCritical, Label: "Local ?", Detail: "нет данных"}
	if c.baseURL == "" {
		rep.Error = "base URL пустой"
		rep.Label = "Local down"
		rep.Detail = rep.Error
		return c.finished(rep)
	}
	if model == "" {
		rep.Error = "модель не выбрана"
		rep.Label = "Local: нет модели"
		rep.Detail = "Выберите модель в Settings"
		rep.Level = HealthBad
		return c.finished(rep)
	}

	origin, err := ollamaOrigin(c.baseURL)
	if err != nil {
		rep.Error = err.Error()
		rep.Label = "Local down"
		rep.Detail = err.Error()
		return c.finished(rep)
	}

	if c.isKnownNotOllama() {
		return c.pingChatOnly(ctx, &rep)
	}

	c.fillVRAM(ctx, origin, &rep)
	// /api/ps ответил 404 — сервер не Ollama (LM Studio, Lemonade, vLLM):
	// /api/generate у них тоже нет, сразу чат-пинг.
	if c.isKnownNotOllama() {
		return c.pingChatOnly(ctx, &rep)
	}

	if err := c.pingGenerate(ctx, origin, &rep); err != nil {
		if err2 := c.pingChat(ctx, &rep); err2 != nil {
			rep.Error = err2.Error()
			rep.Level = HealthCritical
			rep.Label = "Local down"
			rep.Detail = "Сервер не отвечает: " + rep.Error
			return c.finished(rep)
		}
	}

	scoreHealth(&rep)
	return c.finished(rep)
}

// finished проставляет определённый тип сервера: Ollama или OpenAI-совместимый
// (Lemonade, LM Studio, vLLM) — по нему UI и подсказки выбирают формулировки.
func (c *Client) finished(rep HealthReport) HealthReport {
	rep.Kind = c.ServerKind()
	if rep.Kind == "openai" && rep.Detail != "" {
		rep.Detail += " · сервер OpenAI-совместимый (Lemonade/LM Studio/vLLM)"
	}
	return rep
}

// pingChatOnly — health для серверов без Ollama-эндпоинтов: только чат-пинг.
func (c *Client) pingChatOnly(ctx context.Context, rep *HealthReport) HealthReport {
	if err := c.pingChat(ctx, rep); err != nil {
		rep.Error = err.Error()
		rep.Level = HealthCritical
		rep.Label = "Local down"
		rep.Detail = "Сервер не отвечает: " + rep.Error
		return c.finished(*rep)
	}
	scoreHealth(rep)
	return c.finished(*rep)
}

func (c *Client) fillVRAM(ctx context.Context, origin string, rep *HealthReport) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/api/ps", nil)
	if err != nil {
		return
	}
	c.setHeaders(req)
	req.Header.Del("Content-Type")
	res, err := c.http.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return
	}
	if res.StatusCode == http.StatusNotFound {
		// /api/ps есть только у Ollama.
		c.markNotOllama()
		return
	}
	if res.StatusCode >= 300 {
		return
	}
	c.markOllama()
	var ps ollamaPSResponse
	if json.Unmarshal(data, &ps) != nil {
		return
	}
	want := strings.ToLower(strings.TrimSpace(rep.Model))
	for _, m := range ps.Models {
		name := strings.ToLower(strings.TrimSpace(m.Name))
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(m.Model))
		}
		if m.SizeVRAM <= 0 {
			continue
		}
		if want == "" || name == want || strings.HasPrefix(name, want) || strings.HasPrefix(want, name) {
			rep.VRAMLoaded = true
			rep.SizeVRAM = m.SizeVRAM
			return
		}
	}
	for _, m := range ps.Models {
		if m.SizeVRAM > 0 {
			rep.SizeVRAM = m.SizeVRAM
			return
		}
	}
}

func (c *Client) pingGenerate(ctx context.Context, origin string, rep *HealthReport) error {
	body, _ := json.Marshal(map[string]any{
		"model":  rep.Model,
		"prompt": "Reply with exactly one word: ok",
		"stream": false,
		"options": map[string]any{
			"num_predict": 8,
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setHeaders(req)
	start := time.Now()
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	wall := time.Since(start)
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusNotFound {
		// /api/generate есть только у Ollama.
		c.markNotOllama()
	}
	if res.StatusCode >= 300 {
		return mapAPIError(res.StatusCode, data)
	}
	var out ollamaGenerateResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return fmt.Errorf("decode generate: %w", err)
	}
	rep.PingMs = int(wall.Milliseconds())
	if out.LoadDuration > 0 {
		rep.LoadMs = int(out.LoadDuration / int64(time.Millisecond))
	}
	if out.PromptEvalDuration > 0 {
		rep.PrefillMs = int(out.PromptEvalDuration / int64(time.Millisecond))
	}
	if out.EvalDuration > 0 && out.EvalCount > 0 {
		rep.DecodeTokPerSec = float64(out.EvalCount) / (float64(out.EvalDuration) / 1e9)
		rep.TokPerSec = rep.DecodeTokPerSec
	}
	return nil
}

func (c *Client) pingChat(ctx context.Context, rep *HealthReport) error {
	maxTok := 8
	start := time.Now()
	_, err := c.ChatCompletion(ctx, &llm.ChatRequest{
		Model: rep.Model,
		Messages: []llm.Message{
			{Role: "user", Content: "Reply with exactly one word: ok"},
		},
		MaxTokens: &maxTok,
		Stream:    false,
	})
	rep.PingMs = int(time.Since(start).Milliseconds())
	return err
}

func scoreHealth(rep *HealthReport) {
	ping := rep.PingMs
	tps := rep.TokPerSec
	if tps <= 0 && rep.DecodeTokPerSec > 0 {
		tps = rep.DecodeTokPerSec
		rep.TokPerSec = tps
	}
	cold := rep.LoadMs >= 2000 || (!rep.VRAMLoaded && ping >= 1500)

	switch {
	case ping <= 0 && tps <= 0:
		rep.Level = HealthCritical
	case ping >= 15000:
		rep.Level = HealthCritical
	case ping >= 4000 || (tps > 0 && tps < 10):
		rep.Level = HealthBad
	case cold || ping >= 1500 || (tps > 0 && tps < 30):
		rep.Level = HealthWarn
	default:
		rep.Level = HealthOK
	}

	// Prefill vs decode: slow prompt_eval with healthy decode → context/cache issue.
	if rep.Hint == "" && rep.PrefillMs >= 3000 && tps >= 30 {
		rep.Hint = "Prefill медленный при нормальном decode — укоротите контекст (Lite / новый чат) или включите prefix cache на сервере."
		if rep.Level == HealthOK {
			rep.Level = HealthWarn
		}
	}
	if rep.Hint == "" && !rep.VRAMLoaded && tps > 0 && tps < 15 {
		rep.Hint = "Похоже на CPU/offload. Для одного пользователя — Ollama/llama.cpp; для параллели — vLLM + prefix caching."
	}

	parts := make([]string, 0, 5)
	if tps > 0 {
		parts = append(parts, fmt.Sprintf("%.0f tok/s", tps))
	}
	if rep.PrefillMs > 0 {
		if rep.PrefillMs >= 1000 {
			parts = append(parts, fmt.Sprintf("prefill %.1fs", float64(rep.PrefillMs)/1000))
		} else {
			parts = append(parts, fmt.Sprintf("prefill %dms", rep.PrefillMs))
		}
	} else if ping > 0 {
		parts = append(parts, fmt.Sprintf("%d ms", ping))
	}
	if rep.VRAMLoaded {
		parts = append(parts, "VRAM")
	} else if cold {
		parts = append(parts, "cold")
	}
	if len(parts) == 0 {
		rep.Label = "Local ?"
	} else {
		rep.Label = "Local " + strings.Join(parts, " · ")
	}

	var d strings.Builder
	d.WriteString("Проверка локального сервера. ")
	if rep.VRAMLoaded {
		d.WriteString(fmt.Sprintf("Модель в видеопамяти (~%.1f ГБ). ", float64(rep.SizeVRAM)/(1<<30)))
	} else {
		d.WriteString("Модель сейчас не в VRAM (возможен холодный старт). ")
	}
	if tps > 0 {
		d.WriteString(fmt.Sprintf("Decode ≈ %.0f ток/с. ", tps))
	}
	if rep.PrefillMs > 0 {
		d.WriteString(fmt.Sprintf("Prefill ≈ %d мс. ", rep.PrefillMs))
	}
	if ping > 0 {
		d.WriteString(fmt.Sprintf("Пинг (wall) ≈ %d мс. ", ping))
	}
	if rep.LoadMs > 0 {
		d.WriteString(fmt.Sprintf("Загрузка модели ≈ %d мс. ", rep.LoadMs))
	}
	switch rep.Level {
	case HealthOK:
		d.WriteString("Всё хорошо — можно работать.")
	case HealthWarn:
		d.WriteString("Средне: подождите прогрева или проверьте GPU/контекст.")
	case HealthBad:
		d.WriteString("Плохо: очень медленно (часто CPU вместо GPU).")
	default:
		d.WriteString("Критично: сервер еле отвечает или недоступен.")
	}
	if rep.Hint != "" {
		d.WriteString(" ")
		d.WriteString(rep.Hint)
	}
	rep.Detail = d.String()
}
