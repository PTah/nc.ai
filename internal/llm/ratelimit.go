// Edited by nc.ai 1.1.15 on macbook 2026-11-11 21:44:33
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
	// LimitSource — что именно кончилось, если провайдер сказал:
	// "requests" / "tokens" / "credits" (OpenRouter и подобные).
	LimitSource string
}

func (e *RateLimitError) Error() string {
	var b strings.Builder
	b.WriteString(e.Provider)
	b.WriteString(": лимит запросов (429)")
	if e.Wait > 0 {
		b.WriteString(", повтор через ")
		b.WriteString(FormatWait(e.Wait))
	}
	if s := limitSourceRU(e.LimitSource); s != "" {
		b.WriteString(", лимит ")
		b.WriteString(s)
	}
	if d := strings.TrimSpace(e.Detail); d != "" {
		b.WriteString(": ")
		b.WriteString(d)
	}
	b.WriteString(".")
	return b.String()
}

// limitSourceRU переводит машинный limit_source в понятное «по запросам/токенам».
func limitSourceRU(src string) string {
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "":
		return ""
	case "requests", "request", "rpm", "rpd", "per-day", "day":
		return "по запросам"
	case "tokens", "token", "tpm", "tpd":
		return "по токенам"
	case "credits", "credit", "balance", "quota":
		return "по кредитам"
	}
	return "по " + strings.TrimSpace(src)
}

// NewRateLimitError собирает ошибку 429: текст из тела, ожидание из заголовков,
// источник лимита из тела (OpenRouter отдаёт limit_source — по запросам или по
// токенам кончился лимит).
func NewRateLimitError(provider string, h http.Header, body []byte) *RateLimitError {
	detail := APIErrorMessage(body)
	if detail == "" && len(body) > 0 {
		detail = TruncateRunes(strings.TrimSpace(string(body)), 300)
	}
	// Остаток из заголовков («запросы: 5, осталось 0») — это то, по чему видно,
	// действительно ли лимит исчерпан.
	if q := RateLimitQuota(h); q != "" {
		if detail != "" {
			detail += " (" + q + ")"
		} else {
			detail = q
		}
	}
	return &RateLimitError{
		Provider:    provider,
		Detail:      detail,
		Wait:        RetryAfter(h),
		LimitSource: RateLimitSource(body),
	}
}

// RateLimitSource достаёт limit_source из тела ошибки: OpenRouter кладёт его в
// error.metadata.limit_source, часть прокси — в корень.
func RateLimitSource(body []byte) string {
	var payload struct {
		Error struct {
			Metadata struct {
				LimitSource string `json:"limit_source"`
			} `json:"metadata"`
			LimitSource string `json:"limit_source"`
		} `json:"error"`
		LimitSource string `json:"limit_source"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	for _, v := range []string{payload.Error.Metadata.LimitSource, payload.Error.LimitSource, payload.LimitSource} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
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
		if d, err := time.ParseDuration(v); err == nil {
			if d > best {
				best = d
			}
			continue
		}
		// Часть прокси отдаёт unix-время сброса (секунды), часть — просто секунды.
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			var d time.Duration
			if n > 1_000_000_000 {
				d = time.Until(time.Unix(n, 0))
			} else {
				d = time.Duration(n) * time.Second
			}
			if d > best {
				best = d
			}
		}
	}
	// Anthropic: момент сброса окна в RFC3339 (anthropic-ratelimit-*-reset).
	for _, key := range []string{
		"Anthropic-Ratelimit-Requests-Reset",
		"Anthropic-Ratelimit-Tokens-Reset",
		"Anthropic-Ratelimit-Input-Tokens-Reset",
		"Anthropic-Ratelimit-Output-Tokens-Reset",
	} {
		v := strings.TrimSpace(h.Get(key))
		if v == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			if d := time.Until(t); d > best {
				best = d
			}
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
