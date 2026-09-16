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
	Level      HealthLevel `json:"level"`
	Label      string      `json:"label"`
	Detail     string      `json:"detail"`
	Model      string      `json:"model"`
	PingMs     int         `json:"pingMs"`
	TokPerSec  float64     `json:"tokPerSec"`
	VRAMLoaded bool        `json:"vramLoaded"`
	SizeVRAM   int64       `json:"sizeVram"`
	LoadMs     int         `json:"loadMs"`
	Error      string      `json:"error,omitempty"`
}

type ollamaPSResponse struct {
	Models []struct {
		Name     string `json:"name"`
		Model    string `json:"model"`
		SizeVRAM int64  `json:"size_vram"`
	} `json:"models"`
}

type ollamaGenerateResponse struct {
	TotalDuration int64 `json:"total_duration"`
	LoadDuration  int64 `json:"load_duration"`
	EvalCount     int   `json:"eval_count"`
	EvalDuration  int64 `json:"eval_duration"`
}

// ProbeHealth checks VRAM residency (Ollama /api/ps) and a tiny generate/chat ping.
func (c *Client) ProbeHealth(ctx context.Context) HealthReport {
	model := strings.TrimSpace(c.model)
	rep := HealthReport{Model: model, Level: HealthCritical, Label: "Local ?", Detail: "нет данных"}
	if c.baseURL == "" {
		rep.Error = "base URL пустой"
		rep.Label = "Local down"
		rep.Detail = rep.Error
		return rep
	}
	if model == "" {
		rep.Error = "модель не выбрана"
		rep.Label = "Local: нет модели"
		rep.Detail = "Выберите модель в Settings"
		rep.Level = HealthBad
		return rep
	}

	origin, err := ollamaOrigin(c.baseURL)
	if err != nil {
		rep.Error = err.Error()
		rep.Label = "Local down"
		rep.Detail = err.Error()
		return rep
	}

	c.fillVRAM(ctx, origin, &rep)

	if err := c.pingGenerate(ctx, origin, &rep); err != nil {
		if err2 := c.pingChat(ctx, &rep); err2 != nil {
			rep.Error = err2.Error()
			rep.Level = HealthCritical
			rep.Label = "Local down"
			rep.Detail = "Сервер не отвечает: " + rep.Error
			return rep
		}
	}

	scoreHealth(&rep)
	return rep
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
	if err != nil || res.StatusCode >= 300 {
		return
	}
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
	if out.EvalDuration > 0 && out.EvalCount > 0 {
		rep.TokPerSec = float64(out.EvalCount) / (float64(out.EvalDuration) / 1e9)
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

	parts := make([]string, 0, 4)
	if tps > 0 {
		parts = append(parts, fmt.Sprintf("%.0f tok/s", tps))
	}
	if ping > 0 {
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
		d.WriteString(fmt.Sprintf("Скорость генерации ≈ %.0f ток/с. ", tps))
	}
	if ping > 0 {
		d.WriteString(fmt.Sprintf("Пинг ≈ %d мс. ", ping))
	}
	if rep.LoadMs > 0 {
		d.WriteString(fmt.Sprintf("Загрузка модели ≈ %d мс. ", rep.LoadMs))
	}
	switch rep.Level {
	case HealthOK:
		d.WriteString("Всё хорошо — можно работать.")
	case HealthWarn:
		d.WriteString("Средне: подождите прогрева или проверьте GPU.")
	case HealthBad:
		d.WriteString("Плохо: очень медленно (часто CPU вместо GPU).")
	default:
		d.WriteString("Критично: сервер еле отвечает или недоступен.")
	}
	rep.Detail = d.String()
}
