// Edited by nc.ai 1.0.9 on new-admin-pc 2026-11-03 10:54:26
// Edited by nc.ai 1.1.4 on macbook 2026-11-08 19:05:15
package sshx

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Service talks to remote hosts. Keys are resolved from ~/.ssh first
// (Cursor-like), then from the optional app-local Dir.
type Service struct {
	Dir string

	mu    sync.Mutex
	conns map[string]*sshConn
}

type sshConn struct {
	c        *ssh.Client
	lastUsed time.Time
}

// ErrConnLost — соединение с хостом умерло (сон ноутбука, ребут Windows, сброс
// NAT, исчерпаны каналы OpenSSH). Такое соединение нельзя держать в кэше.
var ErrConnLost = errors.New("ssh: соединение потеряно")

// maxCachedConns — верхняя граница открытых логинов: агент ходит на разные хосты
// и с разными ключами, десятки висящих сессий никому не нужны.
const maxCachedConns = 8

// isTransportErr отличает поломку соединения от честного «команда вернула не 0».
// Первое лечится реконнектом, второе — разбором вывода.
func isTransportErr(err error) bool {
	if err == nil {
		return false
	}
	var exit *ssh.ExitError
	if errors.As(err, &exit) {
		return false // удалённая команда отработала и вернула код
	}
	var missing *ssh.ExitMissingError
	if errors.As(err, &missing) {
		return true // «remote command exited without exit status» — канал умер
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection lost",
		"closed network connection",
		"broken pipe",
		"connection reset",
		"unexpected packet",
		"administratively prohibited", // исчерпан MaxSessions на OpenSSH (Windows любит)
		"session not found",
		"eof",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// connKey — ключ переиспользования: хост, порт, пользователь и способ входа.
func connKey(host string, port int, user, authID string) string {
	return fmt.Sprintf("%s|%d|%s|%s", host, port, user, authID)
}

// sshIdleTimeout — сколько держать открытое соединение без команд, прежде чем
// закрыть. Иначе сервер получал бы логин-алерт на каждый вызов ssh_exec.
const sshIdleTimeout = 10 * time.Minute

func New(dir string) *Service {
	s := &Service{Dir: dir, conns: map[string]*sshConn{}}
	go s.reapLoop()
	return s
}

// reapLoop закрывает простаивающие соединения.
func (s *Service) reapLoop() {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		s.mu.Lock()
		for k, e := range s.conns {
			if now.Sub(e.lastUsed) > sshIdleTimeout {
				_ = e.c.Close()
				delete(s.conns, k)
			}
		}
		s.mu.Unlock()
	}
}

func userSSHDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh")
}

func (s *Service) keySearchDirs() []string {
	dirs := make([]string, 0, 2)
	if d := userSSHDir(); d != "" {
		dirs = append(dirs, d)
	}
	if s.Dir != "" && s.Dir != userSSHDir() {
		dirs = append(dirs, s.Dir)
	}
	return dirs
}

func (s *Service) resolveKeyPath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("key name required")
	}
	if filepath.IsAbs(name) {
		if _, err := os.Stat(name); err != nil {
			return "", err
		}
		return name, nil
	}
	for _, dir := range s.keySearchDirs() {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("key %q not found in ~/.ssh (or app ssh dir)", name)
}

// Keygen writes an ed25519 key into ~/.ssh (creates the dir if needed).
func (s *Service) Keygen(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("key name required")
	}
	dir := userSSHDir()
	if dir == "" {
		return "", fmt.Errorf("cannot resolve home directory")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	privPath := filepath.Join(dir, name)
	if _, err := os.Stat(privPath); err == nil {
		return "", fmt.Errorf("key already exists: %s", privPath)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", err
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return "", err
	}
	pubBytes := ssh.MarshalAuthorizedKey(sshPub)
	if err := os.WriteFile(privPath+".pub", pubBytes, 0o644); err != nil {
		return "", err
	}
	return string(pubBytes), nil
}

func loadSigner(path string) (ssh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(data)
}

func (s *Service) Exec(host, user string, port int, keyName, password, command string, timeout time.Duration) (string, error) {
	if host == "" || command == "" {
		return "", fmt.Errorf("host and command required")
	}
	if user == "" {
		user = os.Getenv("USER")
		if user == "" {
			user = os.Getenv("USERNAME")
		}
		if user == "" {
			user = "git"
		}
	}
	if port <= 0 {
		port = 22
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	var auth []ssh.AuthMethod
	if password != "" {
		auth = append(auth, ssh.Password(password))
	}
	if keyName != "" {
		keyPath, err := s.resolveKeyPath(keyName)
		if err != nil {
			return "", err
		}
		signer, err := loadSigner(keyPath)
		if err != nil {
			return "", fmt.Errorf("parse key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if len(auth) == 0 {
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			for _, dir := range s.keySearchDirs() {
				p := filepath.Join(dir, name)
				if signer, err := loadSigner(p); err == nil {
					auth = append(auth, ssh.PublicKeys(signer))
					break
				}
			}
			if len(auth) > 0 {
				break
			}
		}
	}
	if len(auth) == 0 {
		return "", fmt.Errorf("no SSH auth: add a key under ~/.ssh (id_ed25519) or pass key_name/password")
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: hostKeyCallback(),
		Timeout:         timeout,
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	authID := keyName
	if authID == "" && password != "" {
		authID = "password"
	}
	if authID == "" {
		authID = "default"
	}
	key := connKey(host, port, user, authID)

	// Переиспользуем уже открытое соединение: один логин на хост, дальше только
	// каналы. Это убирает спам «Successful SSH login» на каждый шаг агента.
	client, cached, err := s.clientFor(key, addr, cfg)
	if err != nil {
		return "", err
	}
	out, runErr := s.runSession(client, command, timeout)
	if runErr == nil || !isTransportErr(runErr) {
		return out, runErr
	}
	// Соединение умерло — выбрасываем его из кэша и делаем РОВНО один реконнект.
	// Раньше мёртвый клиент оставался в conns навсегда: lastUsed обновлялся каждой
	// попыткой, reapLoop его не видел по простою, и каждый шаг агента падал на
	// NewSession. Новый коннект открывался только со сменой ключа/пароля — снаружи
	// это выглядело как «коннект на каждый запрос» (и так копились логины на Windows).
	s.drop(key, client)
	if !cached {
		return out, runErr // только что открыли и уже сломалось — второй раз не долбим
	}
	fresh, derr := ssh.Dial("tcp", addr, cfg)
	if derr != nil {
		return out, runErr
	}
	s.put(key, fresh)
	return s.runSession(fresh, command, timeout)
}

// clientFor отдаёт живое соединение из кэша или открывает новое.
// Второе значение — было ли оно взято из кэша (нужно, чтобы не реконнектить
// соединение, которое только что установили).
func (s *Service) clientFor(key, addr string, cfg *ssh.ClientConfig) (*ssh.Client, bool, error) {
	s.mu.Lock()
	if e := s.conns[key]; e != nil && e.c != nil {
		e.lastUsed = time.Now()
		c := e.c
		s.mu.Unlock()
		return c, true, nil
	}
	s.mu.Unlock()

	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, false, err
	}
	s.put(key, client)
	return client, false, nil
}

// oldestKey — ключ самого давно использованного соединения (для вытеснения).
func oldestKey(conns map[string]*sshConn) string {
	oldKey := ""
	var oldest time.Time
	for k, e := range conns {
		if oldKey == "" || e.lastUsed.Before(oldest) {
			oldKey, oldest = k, e.lastUsed
		}
	}
	return oldKey
}

// put кладёт соединение в кэш, вытесняя самое давно использованное сверх лимита.
func (s *Service) put(key string, c *ssh.Client) {
	if c == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.conns) >= maxCachedConns {
		oldKey := oldestKey(s.conns)
		if oldKey == "" {
			break
		}
		if e := s.conns[oldKey]; e != nil && e.c != nil {
			_ = e.c.Close()
		}
		delete(s.conns, oldKey)
	}
	s.conns[key] = &sshConn{c: c, lastUsed: time.Now()}
}

// drop убирает соединение из кэша и закрывает его.
func (s *Service) drop(key string, c *ssh.Client) {
	s.mu.Lock()
	if e := s.conns[key]; e != nil && (c == nil || e.c == c) {
		delete(s.conns, key)
	}
	s.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

// runSession открывает канал на уже установленном соединении и исполняет команду.
func (s *Service) runSession(client *ssh.Client, command string, timeout time.Duration) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		// NewSession падает, когда соединение уже мертво или на OpenSSH исчерпан
		// лимит каналов (MaxSessions) — и то и другое лечится новым коннектом.
		return "", fmt.Errorf("%w: %v", ErrConnLost, err)
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	// Удалённая команда тоже должна укладываться в лимит: ClientConfig.Timeout
	// ограничивает только dial и handshake, а `tail -f` или зависший apt держали
	// бы агент бесконечно.
	runDone := make(chan error, 1)
	go func() { runDone <- session.Run(command) }()

	var runErr error
	timedOut := false
	select {
	case runErr = <-runDone:
	case <-time.After(timeout):
		timedOut = true
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		// Соединение не закрываем: оно общее, его приберёт reapLoop по простою.
		select {
		case runErr = <-runDone:
		case <-time.After(2 * time.Second):
			// Сессия не отвечает даже после закрытия — отдаём то, что успели прочитать.
		}
	}

	out := fmt.Sprintf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	if timedOut {
		return out + fmt.Sprintf("\nERROR: удалённая команда не уложилась в %s — SSH-сессия закрыта", timeout), nil
	}
	if runErr != nil {
		if isTransportErr(runErr) {
			return out, fmt.Errorf("%w: %v", ErrConnLost, runErr)
		}
		return out + "\nERROR: " + runErr.Error(), nil
	}
	return out, nil
}

func (s *Service) ListKeys() ([]string, error) {
	seen := map[string]struct{}{}
	names := make([]string, 0)
	for _, dir := range s.keySearchDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if filepath.Ext(name) == ".pub" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return names, nil
}

// hostKeyCallback verifies the server key against ~/.ssh/known_hosts and
// falls back to TOFU (trust-on-first-use): an unseen host key is remembered
// after the first successful connect, changes are rejected (MITM protection).
func hostKeyCallback() ssh.HostKeyCallback {
	knownHostsPath := filepath.Join(userSSHDir(), "known_hosts")
	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		// No known_hosts file: use TOFU — record the key on first connect.
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return appendKnownHost(hostname, remote, key)
		}
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := cb(hostname, remote, key)
		if isUnknownHostErr(err) {
			return appendKnownHost(hostname, remote, key)
		}
		return err
	}
}

func isUnknownHostErr(err error) bool {
	if err == nil {
		return false
	}
	var khe *knownhosts.KeyError
	return errors.As(err, &khe) && len(khe.Want) == 0
}

func appendKnownHost(hostname string, remote net.Addr, key ssh.PublicKey) error {
	dir := userSSHDir()
	if dir == "" {
		return fmt.Errorf("cannot resolve ~/.ssh for known_hosts")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	entry := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	f, err := os.OpenFile(filepath.Join(dir, "known_hosts"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(entry + "\n"); err != nil {
		return err
	}
	return nil
}
