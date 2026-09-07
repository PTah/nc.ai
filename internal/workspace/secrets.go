package workspace

import (
	"fmt"
	"regexp"
	"strings"
)

// secretFileRe matches file names that usually carry credentials and must not
// be sent to LLM providers verbatim.
var secretFileRe = regexp.MustCompile(`(?i)(^|[\\/])(\.env(\..+)?|.*secret.*|.*credential.*|id_rsa|id_ed25519|\.npmrc|\.netrc|\.pypirc|\.aws[\\/]credentials|.*\.pem|.*\.key)$`)

// IsSecretPath reports whether rel looks like a credentials file.
func IsSecretPath(rel string) bool {
	return secretFileRe.MatchString(strings.TrimSpace(rel))
}

var secretValueRe = regexp.MustCompile(`(?im)^([ \t]*)([A-Za-z0-9_.\-]*(?:KEY|TOKEN|SECRET|PASSWORD|PASSWD|PWD|CREDENTIAL)[A-Za-z0-9_.\-]*[ \t]*[:=][ \t]*)("([^"]{4,})"|(?:'([^']{4,})'|([^\s"']{4,})))`)

// MaskSecrets replaces values of secret-looking KEY=VALUE pairs with ***.
// Keys themselves stay visible so the model knows the file layout.
func MaskSecrets(content string) string {
	if !looksSecretish(content) {
		return content
	}
	return secretValueRe.ReplaceAllString(content, "$1$2***")
}

func looksSecretish(content string) bool {
	l := strings.ToLower(content)
	return strings.Contains(l, "key") || strings.Contains(l, "token") ||
		strings.Contains(l, "secret") || strings.Contains(l, "password") ||
		strings.Contains(l, "passwd") || strings.Contains(l, "credential")
}

// guardRead applies the secret policy for agent reads of one file.
func (m *Manager) guardRead(rel string, content string) (string, error) {
	if !IsSecretPath(rel) {
		return MaskSecrets(content), nil
	}
	return fmt.Sprintf(
		"Доступ к файлу %s ограничен: он может содержать секреты (ключи, пароли, токены). "+
			"Содержимое не передаётся модели. Если пользователю нужен доступ, попросите его вставить значение вручную.",
		rel,
	), nil
}
