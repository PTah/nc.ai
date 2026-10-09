package redact

import (
	"regexp"
	"strings"
)

var (
	bearerRe = regexp.MustCompile(`(?i)(bearer\s+)[a-z0-9._\-+=/]{8,}`)
	skRe     = regexp.MustCompile(`(?i)\b(sk-[a-z0-9_\-]{8,})`)
	zaiRe    = regexp.MustCompile(`(?i)\b(zai-[a-z0-9_\-]{8,})`)
	// urlCredRe masks credentials embedded in URLs: scheme://user:pass@host.
	urlCredRe = regexp.MustCompile(`(?i)([a-z][a-z0-9+.\-]*://)([^\s/@]+)@`)
	// basicRe masks Authorization: Basic <base64>.
	basicRe = regexp.MustCompile(`(?i)(authorization:\s*basic\s+)[a-z0-9+/=]{6,}`)
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
	out = urlCredRe.ReplaceAllString(out, "${1}***@")
	out = basicRe.ReplaceAllString(out, "${1}***")
	out = keyEqRe.ReplaceAllString(out, "${1}***")
	return out
}

// LooksSecret reports whether s likely contains a credential worth masking.
func LooksSecret(s string) bool {
	l := strings.ToLower(s)
	if strings.Contains(l, "bearer ") || strings.Contains(l, "sk-") || strings.Contains(l, "zai-") {
		return true
	}
	if strings.Contains(l, "authorization:") && strings.Contains(l, "basic") {
		return true
	}
	if strings.Contains(l, "://") {
		if i := strings.Index(l, "://"); i >= 0 {
			rest := l[i+3:]
			if at := strings.Index(rest, "@"); at >= 0 {
				if slash := strings.Index(rest, "/"); slash < 0 || at < slash {
					return true
				}
			}
		}
	}
	return strings.Contains(l, "api_key") || strings.Contains(l, "api-key") ||
		strings.Contains(l, "begin ") && strings.Contains(l, "private key")
}

var (
	// envAssignRe matches NAME=value / NAME: value, including $env:NAME and $NAME.
	envAssignRe = regexp.MustCompile(`(?i)((?:\$env:|\$)?[\pL_][\pL0-9_]*)(\s*[:=]\s*)("[^"\n]*"|'[^'\n]*'|[^\s;&|(){}]{1,})`)
	// jsonKeyRe matches "key": value pairs in JSON tool arguments, where the key
	// is wrapped in quotes (e.g. {"user": "papatramp"}).
	jsonKeyRe = regexp.MustCompile(`(?i)("[^"\n]*?(?:password|passwd|passphrase|pwd|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|bearer|session[_-]?id|cookie|login|username|user|парол\pL*|логин\pL*|пользовател\pL*)[^"\n]*?")(\s*:\s*)("(?:[^"\\\n]|\\.)*"|[^\s,;}]+)`)
	// secretNameRe matches assignment keys that usually hold credentials.
	secretNameRe = regexp.MustCompile(`(?i)(password|passwd|passphrase|pwd|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|bearer|session[_-]?id|cookie|login|username|user|парол\pL*|логин\pL*|пользовател\pL*)`)
	// flagAssignRe matches CLI flags like --password x, -p=x, /password:x.
	flagAssignRe = regexp.MustCompile(`(?i)((?:--?|/)(?:password|passwd|passphrase|pwd|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credential|login|username|user))(\s+|[:=]\s*)("[^"\n]*"|'[^'\n]*'|[^\s;&|(){}]{2,})`)
)

// Display is a stronger mask used only for what the user sees in the chat.
// Besides everything String hides, it also masks values of environment-style
// (ALL_CAPS / $env:) and credential-named assignments, plus secret CLI flags.
// The real values still run and still reach the model; only the transcript text
// is redacted, so secrets are usable but not shown.
func Display(s string) string {
	if s == "" {
		return s
	}
	out := String(s)
	out = jsonKeyRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := jsonKeyRe.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		return sub[1] + sub[2] + maskValue(sub[3])
	})
	out = envAssignRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := envAssignRe.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		name, op, val := sub[1], sub[2], sub[3]
		raw := strings.Trim(val, "\"'")
		mask := false
		switch {
		case strings.HasPrefix(name, "$"): // $env:NAME= / $NAME= is always a command assignment
			mask = true
		case secretNameRe.MatchString(name):
			mask = true
		case envStyleName(name) && looksSecretishValue(raw): // ZP='[...]' style
			mask = true
		}
		if mask {
			return name + op + maskValue(val)
		}
		return m
	})
	out = flagAssignRe.ReplaceAllStringFunc(out, func(m string) string {
		sub := flagAssignRe.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		return sub[1] + sub[2] + maskValue(sub[3])
	})
	return out
}

// envStyleName reports whether name looks like an environment variable
// (ALL_CAPS_WITH_UNDERSCORES), e.g. ZP, PYTHONIOENCODING, DATABASE_URL.
func envStyleName(name string) bool {
	name = strings.TrimPrefix(name, "$env:")
	name = strings.TrimPrefix(name, "$")
	if name == "" {
		return false
	}
	hasLetter := false
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9', r == '_':
			// digits and underscores are fine inside an env-style name
		default:
			return false
		}
	}
	return hasLetter
}

func maskValue(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return string(v[0]) + "***" + string(v[0])
	}
	return "***"
}

// looksSecretishValue reports whether a value assigned to an ALL_CAPS name looks
// like a credential rather than a plain word, so prose like "NOTE: something"
// is not mangled while "ZP=[eqyfhskj]" still gets masked.
func looksSecretishValue(v string) bool {
	if len(v) < 4 {
		return false
	}
	for _, r := range v {
		if r >= '0' && r <= '9' {
			return true
		}
		switch r {
		case '[', ']', '{', '}', '(', ')', '!', '@', '#', '$', '%', '^', '&', '*', '+', '=', '|', ';', ':', '<', '>', '?', '~', '`', '\'', '"':
			return true
		}
	}
	return false
}
