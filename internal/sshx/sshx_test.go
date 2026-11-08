// Created by nc.ai 1.1.4 on macbook 2026-11-08 19:05:48
package sshx

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Ошибки соединения и честный «команда вернула код» должны различаться: первое
// лечится одним реконнектом, второе — разбором вывода. Раньше мёртвый клиент
// оставался в кэше, и каждый шаг агента падал на NewSession, открывая новые
// логины; на Windows это ещё и упиралось в лимит каналов OpenSSH.
func TestIsTransportErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"connection lost", errors.New("ssh: connection lost"), true},
		{"closed network connection", errors.New("use of closed network connection"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"connection reset", fmt.Errorf("read tcp: %w", errors.New("connection reset by peer")), true},
		{"maxsessions", errors.New("ssh: rejected: administratively prohibited (open failed)"), true},
		{"eof", errors.New("EOF"), true},
		{"exit missing", &ssh.ExitMissingError{}, true},
		{"обычная ошибка удалённой команды", errors.New("Process exited with status 1"), false},
		{"ошибка разбора аргументов", errors.New("host and command required"), false},
	}
	for _, c := range cases {
		if got := isTransportErr(c.err); got != c.want {
			t.Errorf("%s: isTransportErr(%v) = %v, ожидали %v", c.name, c.err, got, c.want)
		}
	}
}

// Ключ кэша стабилен: иначе одно и то же соединение не переиспользуется и на
// каждый вызов уходит новый логин.
func TestConnKeyStable(t *testing.T) {
	a := connKey("192.168.1.10", 22, "user", "default")
	b := connKey("192.168.1.10", 22, "user", "default")
	if a != b {
		t.Fatalf("ключ нестабилен: %q != %q", a, b)
	}
	if a == connKey("192.168.1.10", 2222, "user", "default") {
		t.Error("порт должен входить в ключ")
	}
	if a == connKey("192.168.1.10", 22, "user", "password") {
		t.Error("способ входа должен входить в ключ: ключ и пароль — разные соединения")
	}
}

// Вытеснение из кэша выбирает самое давно использованное соединение: агент может
// ходить на разные хосты, десятки висящих логинов не нужны.
func TestOldestKey(t *testing.T) {
	base := time.Now()
	conns := map[string]*sshConn{
		"fresh":   {lastUsed: base},
		"stale":   {lastUsed: base.Add(-time.Hour)},
		"average": {lastUsed: base.Add(-time.Minute)},
	}
	if got := oldestKey(conns); got != "stale" {
		t.Fatalf("oldestKey = %q, ожидали \"stale\"", got)
	}
	if got := oldestKey(map[string]*sshConn{}); got != "" {
		t.Fatalf("для пустого кэша oldestKey = %q, ожидали пустую строку", got)
	}
}
