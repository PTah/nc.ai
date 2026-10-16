// Edited by nc.ai 0.8.7.7 on macbook 2026-10-16 22:51:13
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
		`$env:ZP='[topsecret]'; $env:PYTHONIOENCODING='utf-8'`,
		`PASSWORD=secret123 python app.py`,
		`run --password hunter2 --user admin`,
		`prog /password:hunter2`,
		`{"command":"export TOKEN=abcdef123456"}`,
		`логин=вася пароль=секрет123`,
	}
	for _, in := range cases {
		out := Display(in)
		for _, leak := range []string{"[topsecret]", "secret123", "hunter2", "abcdef123456", "вася", "секрет123"} {
			if strings.Contains(out, leak) {
				t.Fatalf("Display leaked %q in %q", leak, out)
			}
		}
	}
}

func TestDisplayMasksJSONToolArgs(t *testing.T) {
	in := `{"host":"192.168.128.1","user":"papatramp","password":"[topsecret]","command":"uname -a"}`
	out := Display(in)
	for _, leak := range []string{"papatramp", "[topsecret]"} {
		if strings.Contains(out, leak) {
			t.Fatalf("Display leaked %q in %q", leak, out)
		}
	}
	if !strings.Contains(out, `"host":"192.168.128.1"`) {
		t.Fatalf("Display should keep non-secret host, got %q", out)
	}
}

func TestStringMasksURLCredsAndBasic(t *testing.T) {
	cases := map[string]string{
		"https://user:pass@example.com/repo":                        "https://***@example.com/repo",
		"git clone https://papatramp:secret@git.papatramp.ru/x.git": "git clone https://***@git.papatramp.ru/x.git",
		"Authorization: Basic dXNlcjpwYXNz":                         "Authorization: Basic ***",
	}
	for in, want := range cases {
		if got := String(in); got != want {
			t.Fatalf("String(%q) = %q, want %q", in, got, want)
		}
	}
	// scp-синтаксис без пароля не трогаем.
	if got := String("git@github.com:PTah/nc.ai.git"); got != "git@github.com:PTah/nc.ai.git" {
		t.Fatalf("scp URL changed: %q", got)
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

// resetRemembered нужен тестам: список значений общий для процесса.
func resetRemembered() {
	rememberMu.Lock()
	defer rememberMu.Unlock()
	remembered = nil
	seenSecret = map[string]bool{}
}

func TestSecretsFromFreeFormMessage(t *testing.T) {
	got := Secrets("логин vasya83 и пароль hunter2secret, внеси его в файл logins")
	want := []string{"vasya83", "hunter2secret"}
	if len(got) != len(want) {
		t.Fatalf("Secrets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Secrets = %v, want %v", got, want)
		}
	}
}

func TestSecretsIgnoresPlainRussian(t *testing.T) {
	if got := Secrets("внеси его в файл logins и добавь проверку бэкапов"); len(got) != 0 {
		t.Fatalf("в обычной фразе нет секретов, а нашли %v", got)
	}
}

// Главное требование: то, что пользователь прислал в окне ввода, не должно
// появляться в переписке — даже если агент повторит значение без имени и флагов.
func TestDisplayHidesValuesFromUserMessage(t *testing.T) {
	resetRemembered()
	defer resetRemembered()

	msg := "логин vasya83 и пароль hunter2secret"
	Remember(Secrets(msg)...)

	if got := Display(msg); containsAny(got, "vasya83", "hunter2secret") {
		t.Fatalf("сообщение пользователя показано без маски: %q", got)
	}
	cmd := `printf '%s\n' "192.168.1.10|22|vasya83|hunter2secret|home gitea" > ~/CursorRules/local/logins`
	if got := Display(cmd); containsAny(got, "vasya83", "hunter2secret") {
		t.Fatalf("значения из окна ввода уехали в команду: %q", got)
	}
	// Модель по-прежнему получает оригинал: String не трогает запомненное.
	if got := String(cmd); !strings.Contains(got, "hunter2secret") {
		t.Fatalf("String не должен маскировать значения для модели: %q", got)
	}
}

func TestDisplayHidesJSONAndFlagValuesFromUserMessage(t *testing.T) {
	resetRemembered()
	defer resetRemembered()

	Remember(Secrets(`{"user":"vasya83","password":"hunter2secret"}`)...)
	Remember(Secrets("заходи с --password hunter2secret и логином vasya83")...)

	for _, in := range []string{
		"логин vasya83, пароль hunter2secret",
		`curl -u vasya83:hunter2secret http://host/`,
		"поля: vasya83 | hunter2secret",
	} {
		if got := Display(in); containsAny(got, "vasya83", "hunter2secret") {
			t.Fatalf("Display leaked в %q -> %q", in, got)
		}
	}
}
