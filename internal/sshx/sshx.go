package sshx

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

type Service struct {
	Dir string
}

func New(dir string) *Service {
	return &Service{Dir: dir}
}

func (s *Service) ensureDir() error {
	if s.Dir == "" {
		return fmt.Errorf("ssh dir not configured")
	}
	return os.MkdirAll(s.Dir, 0o700)
}

func (s *Service) Keygen(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("key name required")
	}
	if err := s.ensureDir(); err != nil {
		return "", err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return "", err
	}
	privPath := filepath.Join(s.Dir, name)
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

func (s *Service) Exec(host, user string, port int, keyName, password, command string, timeout time.Duration) (string, error) {
	if host == "" || command == "" {
		return "", fmt.Errorf("host and command required")
	}
	if user == "" {
		user = "root"
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
		keyPath := filepath.Join(s.Dir, keyName)
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			return "", fmt.Errorf("read key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return "", fmt.Errorf("parse key: %w", err)
		}
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if len(auth) == 0 {
		def := filepath.Join(s.Dir, "id_ed25519")
		if data, err := os.ReadFile(def); err == nil {
			if signer, err := ssh.ParsePrivateKey(data); err == nil {
				auth = append(auth, ssh.PublicKeys(signer))
			}
		}
	}
	if len(auth) == 0 {
		return "", fmt.Errorf("no SSH auth: provide password or key_name")
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
	if err := s.ensureDir(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) == ".pub" {
			continue
		}
		names = append(names, name)
	}
	return names, nil
}
