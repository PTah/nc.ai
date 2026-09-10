package redact

import (
	"regexp"
	"strings"
)

var (
	bearerRe = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._\-+=/]{8,}`)
	skRe     = regexp.MustCompile(`(?i)\b(sk-[a-z0-9_\-]{8,})`)
	zaiRe    = regexp.MustCompile(`(?i)\b(zai-[a-z0-9_\-]{8,})`)
	keyEqRe  = regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|secret|password|passwd|authorization)["']?\s*[:=]\s*["']?)([^\s"'\\]{6,})`)
	pemRe    = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
)

// String masks credentials that must not leak into LLM context, logs, or UI.
func String(s string) string {
	if s == "" {
		return s
	}
	out := pemRe.ReplaceAllString(s, "-----BEGIN PRIVATE KEY-----\n***\n-----END PRIVATE KEY-----")
	out = bearerRe.ReplaceAllString(out, "${1}***")
	out = skRe.ReplaceAllString(out, "sk-***")
	out = zaiRe.ReplaceAllString(out, "zai-***")
	out = keyEqRe.ReplaceAllString(out, "${1}***")
	return out
}

// LooksSecret reports whether s likely contains a credential worth masking.
func LooksSecret(s string) bool {
	l := strings.ToLower(s)
	if strings.Contains(l, "bearer ") || strings.Contains(l, "sk-") || strings.Contains(l, "zai-") {
		return true
	}
	return strings.Contains(l, "api_key") || strings.Contains(l, "api-key") ||
		strings.Contains(l, "begin ") && strings.Contains(l, "private key")
}
