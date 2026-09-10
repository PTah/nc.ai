package redact

import "testing"

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
