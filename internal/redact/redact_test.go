package redact

import (
	"strings"
	"testing"
)

func TestStringMasksSecrets(t *testing.T) {
	in := "Authorization: Bearer abcdefghijklmnop\nkey=sk-live-ABCDEFGH\nAPI_KEY=hunter2secret\nzai-abcdef123456"
	out := String(in)
	if containsAny(out, "abcdefghijklmnop", "sk-live-ABCDEFGH", "hunter2secret", "zai-abcdef123456") {
		t.Fatalf("leaked secret: %s", out)
	}
	if !containsAny(out, "***") {
		t.Fatalf("expected mask, got %s", out)
	}
}

func TestStringLeavesNormalText(t *testing.T) {
	in := "func Routes() {}\nhello world"
	if String(in) != in {
		t.Fatalf("got %q", String(in))
	}
}

func TestDisplayMasksCommandSecrets(t *testing.T) {
	cases := []string{
		`$env:ZP='[eqyfhskj]'; $env:PYTHONIOENCODING='utf-8'`,
		`PASSWORD=secret123 python app.py`,
		`run --password hunter2 --user admin`,
		`prog /password:hunter2`,
		`{"command":"export TOKEN=abcdef123456"}`,
		`логин=вася пароль=секрет123`,
	}
	for _, in := range cases {
		out := Display(in)
		for _, leak := range []string{"[eqyfhskj]", "secret123", "hunter2", "abcdef123456", "вася", "секрет123"} {
			if strings.Contains(out, leak) {
				t.Fatalf("Display leaked %q in %q", leak, out)
			}
		}
	}
}

func TestDisplayKeepsNormalText(t *testing.T) {
	for _, in := range []string{
		"func Routes() {}",
		"host=db.example.com",
		"url = https://example.com/x",
		"cd /users/foo && go test ./...",
		"### NOTE: something",
		"HEADERS: Content-Type",
	} {
		if got := Display(in); got != in {
			t.Fatalf("Display changed normal text: %q -> %q", in, got)
		}
	}
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && len(s) >= len(n) {
			for i := 0; i+len(n) <= len(s); i++ {
				if s[i:i+len(n)] == n {
					return true
				}
			}
		}
	}
	return false
}
