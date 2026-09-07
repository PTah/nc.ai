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
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Service talks to remote hosts. Keys are resolved from ~/.ssh first
// (Cursor-like), then from the optional app-local Dir.
type Service struct {
	Dir string // optional app-local fallback (NotCursor/ssh)
}

func New(dir string) *Service {
	return &Service{Dir: dir}
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
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	runErr := session.Run(command)
	out := fmt.Sprintf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	if runErr != nil {
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
