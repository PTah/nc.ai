package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimitError — провайдер ответил 429. Агент упирается в лимиты постоянно:
// у бесплатных тарифов (Routeway, Atria, Selora, ShareLLM) это 5 RPM / 200 RPD,
// и без подсказки «когда повторять» прогон обрывается на середине.
type RateLimitError struct {
	Provider string
	Detail   string
	Wait     time.Duration // 0 — провайдер не сказал, когда можно повторить
}

func (e *RateLimitError) Error() string {
	var b strings.Builder
	b.WriteString(e.Provider)
	b.WriteString(": лимит запросов (429)")
	if e.Wait > 0 {
		b.WriteString(", повтор через ")
		b.WriteString(FormatWait(e.Wait))
	}
	if d := strings.TrimSpace(e.Detail); d != "" {
		b.WriteString(": ")
		b.WriteString(d)
	}
	b.WriteString(".")
	return b.String()
}

// FormatWait печатает длительность по-человечески: «42 сек», «2 мин 5 сек».
func FormatWait(d time.Duration) string {
	if d <= 0 {
		return "0 сек"
	}
	d = d.Round(time.Second)
	if d < time.Second {
		// Реальное ожидание округляем вверх: «0 сек» в сообщении о лимите
		// выглядит как ошибка.
		return "1 сек"
	}
	if d < time.Minute {
		return fmt.Sprintf("%d сек", int(d.Seconds()))
	}
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	if s == 0 {
		return fmt.Sprintf("%d мин", m)
	}
	return fmt.Sprintf("%d мин %d сек", m, s)
}

// RetryAfter — когда провайдер разрешит следующий запрос. Сначала смотрим
// Retry-After (секунды или HTTP-дата), затем x-ratelimit-reset-* в формате
// OpenAI («6m0s»). Из нескольких значений берём самое позднее.
func RetryAfter(h http.Header) time.Duration {
	if h == nil {
		return 0
	}
	var best time.Duration
	if v := strings.TrimSpace(h.Get("Retry-After")); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			best = time.Duration(secs) * time.Second
		} else if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > best {
				best = d
			}
		}
	}
	for _, key := range []string{
		"X-RateLimit-Reset-Requests",
		"X-RateLimit-Reset-Tokens",
		"X-RateLimit-Reset",
		"X-Rpm-Limit-Reset",
	} {
		v := strings.TrimSpace(h.Get(key))
		if v == "" {
			continue
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			continue
		}
		if d > best {
			best = d
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

// RateLimitQuota — остаток из заголовков лимитов («лимит 5 запросов, осталось 0»).
func RateLimitQuota(h http.Header) string {
	if h == nil {
		return ""
	}
	limit := firstHeader(h, "X-RateLimit-Limit-Requests", "X-RateLimit-Limit", "X-Rpm-Limit")
	left := firstHeader(h, "X-RateLimit-Remaining-Requests", "X-RateLimit-Remaining", "X-Rpm-Remaining")
	limitTok := firstHeader(h, "X-RateLimit-Limit-Tokens")
	leftTok := firstHeader(h, "X-RateLimit-Remaining-Tokens")
	var parts []string
	if limit != "" || left != "" {
		parts = append(parts, fmt.Sprintf("запросы: %s, осталось %s", orDash(limit), orDash(left)))
	}
	if limitTok != "" || leftTok != "" {
		parts = append(parts, fmt.Sprintf("токены: %s, осталось %s", orDash(limitTok), orDash(leftTok)))
	}
	return strings.Join(parts, "; ")
}

// APIErrorMessage достаёт человекочитаемое сообщение об ошибке из тела ответа
// ({"error":{"message":…}}, {"error":"…"}, {"message":…}).
func APIErrorMessage(body []byte) string {
	var nested struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &nested); err == nil {
		if s := strings.TrimSpace(nested.Error.Message); s != "" {
			return s
		}
		if s := strings.TrimSpace(nested.Message); s != "" {
			return s
		}
	}
	var flat struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &flat) == nil {
		if s := strings.TrimSpace(flat.Error); s != "" {
			return s
		}
	}
	return ""
}

func firstHeader(h http.Header, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(h.Get(k)); v != "" {
			return v
		}
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}
