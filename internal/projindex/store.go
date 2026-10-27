// Created by nc.ai 0.8.9.7 on new-admin-pc 2026-10-28 09:24:59
package projindex

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"notcursor.ai/app/internal/fsx"
)

// Store keeps one index per project root: in memory for the session and on
// disk between restarts. Rebuilds are incremental — only files whose size or
// mtime changed are parsed again.
type Store struct {
	mu     sync.Mutex
	byRoot map[string]*Index
	dir    string // disk cache directory; "" = memory only
}

// New returns a store; dir may be "" to disable persistence.
func New(dir string) *Store {
	return &Store{byRoot: map[string]*Index{}, dir: dir}
}

var (
	defaultOnce sync.Once
	defaultSt   *Store
)

// Default is the process-wide store used by the agent tools. It persists the
// index under the OS cache directory so a restart does not re-parse the repo.
func Default() *Store {
	defaultOnce.Do(func() { defaultSt = New(cacheDir()) })
	return defaultSt
}

// cacheDir is <os cache>/nc.ai/projindex, or "" when the OS reports no cache dir.
func cacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		return ""
	}
	return filepath.Join(base, "nc.ai", "projindex")
}

// Get returns the index for root. It only re-parses files that changed since
// the previous call, so repeated tool calls during one run are cheap.
func (s *Store) Get(root string) (*Index, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil, fmt.Errorf("no project root")
	}
	sc, err := scan(root)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.byRoot[root]
	if prev != nil && prev.FP == sc.fp {
		return prev, nil
	}
	if prev == nil {
		prev = s.loadDisk(root)
	}
	idx := build(root, sc, prev)
	if s.dir != "" {
		s.saveDisk(idx)
	}
	s.byRoot[root] = idx
	return idx, nil
}

// Invalidate drops the cached index for root (used after a big write).
func (s *Store) Invalidate(root string) {
	root = filepath.Clean(strings.TrimSpace(root))
	s.mu.Lock()
	delete(s.byRoot, root)
	s.mu.Unlock()
}

// diskEnvelope versions the persisted index so an extractor change cannot
// resurrect stale symbols.
type diskEnvelope struct {
	Version int    `json:"version"`
	Index   *Index `json:"index"`
}

func (s *Store) pathFor(root string) string {
	sum := sha1.Sum([]byte(root))
	return filepath.Join(s.dir, hex.EncodeToString(sum[:8])+".json")
}

func (s *Store) loadDisk(root string) *Index {
	if s.dir == "" {
		return nil
	}
	raw, err := os.ReadFile(s.pathFor(root))
	if err != nil {
		return nil
	}
	var env diskEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	if env.Version != cacheVersion || env.Index == nil || env.Index.Root != root {
		return nil
	}
	return env.Index
}

func (s *Store) saveDisk(idx *Index) {
	if s.dir == "" || idx == nil {
		return
	}
	raw, err := json.Marshal(diskEnvelope{Version: cacheVersion, Index: idx})
	if err != nil {
		return
	}
	_ = fsx.WriteFileAtomic(s.pathFor(idx.Root), raw, 0o600)
}
