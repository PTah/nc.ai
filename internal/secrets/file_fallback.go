package secrets

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"notcursor.ai/app/internal/fsx"
)

type fileBackend struct {
	path string
	mu   sync.Mutex
}

func newFileBackend(path string) *fileBackend {
	return &fileBackend{path: path}
}

func (f *fileBackend) load() (map[string]string, error) {
	data, err := os.ReadFile(f.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]string{}
	}
	return out, nil
}

func (f *fileBackend) save(m map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fsx.WriteFileAtomic(f.path, data, 0o600)
}

func (f *fileBackend) Get(id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return "", err
	}
	v := m[id]
	if v == "" {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *fileBackend) Set(id, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	m[id] = secret
	return f.save(m)
}

func (f *fileBackend) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := m[id]; !ok {
		return ErrNotFound
	}
	delete(m, id)
	if len(m) == 0 {
		_ = os.Remove(f.path)
		return nil
	}
	return f.save(m)
}
