package secrets

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	Service = "NotCursor.ai"

	IDDeepSeek   = "deepseek"
	IDZai        = "zai"
	IDOpenRouter = "openrouter"
)

var (
	ErrNotFound = errors.New("secret not found")
	errNoOS     = errors.New("no os secret store")
)

type backend interface {
	Get(id string) (string, error)
	Set(id, secret string) error
	Delete(id string) error
}

// Store prefers the OS vault (Keychain / Credential Manager) and falls back
// to a 0600 file under the app data dir.
type Store struct {
	os   backend
	file backend
}

// Open uses the OS store when available, plus a file fallback in appDataDir.
func Open(appDataDir string) *Store {
	return &Store{
		os:   newOSBackend(),
		file: newFileBackend(filepath.Join(appDataDir, "secrets.json")),
	}
}

// OpenFileOnly never touches the OS vault (tests).
func OpenFileOnly(appDataDir string) *Store {
	return &Store{
		os:   nopBackend{},
		file: newFileBackend(filepath.Join(appDataDir, "secrets.json")),
	}
}

func NormalizeID(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case IDZai:
		return IDZai
	case IDOpenRouter:
		return IDOpenRouter
	default:
		return IDDeepSeek
	}
}

func (s *Store) Get(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", ErrNotFound
	}
	if v, err := s.os.Get(id); err == nil && strings.TrimSpace(v) != "" {
		return v, nil
	}
	return s.file.Get(id)
}

func (s *Store) Set(id, secret string) error {
	id = strings.TrimSpace(id)
	secret = strings.TrimSpace(secret)
	if id == "" {
		return errors.New("secret id is empty")
	}
	if secret == "" {
		return s.Delete(id)
	}
	osErr := s.os.Set(id, secret)
	if osErr == nil {
		_ = s.file.Delete(id)
		return nil
	}
	if err := s.file.Set(id, secret); err != nil {
		if osErr != nil && !errors.Is(osErr, errNoOS) {
			return osErr
		}
		return err
	}
	return nil
}

func (s *Store) Delete(id string) error {
	id = strings.TrimSpace(id)
	osErr := s.os.Delete(id)
	fileErr := s.file.Delete(id)
	if fileErr != nil && !errors.Is(fileErr, ErrNotFound) {
		return fileErr
	}
	if osErr != nil && !errors.Is(osErr, ErrNotFound) && !errors.Is(osErr, errNoOS) {
		return osErr
	}
	return nil
}

// BackendLabel is a short UI string for where keys live on this OS.
func BackendLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS Keychain"
	case "windows":
		return "Windows Credential Manager"
	default:
		return "локальный файл (права 0600)"
	}
}

type nopBackend struct{}

func (nopBackend) Get(string) (string, error) { return "", errNoOS }
func (nopBackend) Set(string, string) error   { return errNoOS }
func (nopBackend) Delete(string) error        { return errNoOS }
