package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectTree builds a compact directory map (names only) for agent orientation.
// maxEntries caps listed files/dirs; deeper content is summarized as "…".
func (m *Manager) ProjectTree(maxEntries int) (string, error) {
	root, err := m.ActiveRoot()
	if err != nil {
		return "", err
	}
	if maxEntries <= 0 {
		maxEntries = 400
	}
	var b strings.Builder
	b.WriteString(filepath.Base(root) + "/\n")
	count := 0
	var walk func(dir, prefix string) error
	walk = func(dir, prefix string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		var dirs, files []os.DirEntry
		for _, e := range entries {
			if ignoredName(e.Name()) {
				continue
			}
			if e.IsDir() {
				dirs = append(dirs, e)
			} else {
				files = append(files, e)
			}
		}
		all := append(dirs, files...)
		for i, e := range all {
			if count >= maxEntries {
				remaining := len(all) - i
				fmt.Fprintf(&b, "%s… (+%d more)\n", prefix, remaining)
				return errSearchLimit
			}
			last := i == len(all)-1
			branch := "├── "
			nextPref := prefix + "│   "
			if last {
				branch = "└── "
				nextPref = prefix + "    "
			}
			name := e.Name()
			if e.IsDir() {
				fmt.Fprintf(&b, "%s%s%s/\n", prefix, branch, name)
				count++
				if err := walk(filepath.Join(dir, name), nextPref); err != nil {
					if err == errSearchLimit {
						return err
					}
				}
				continue
			}
			fmt.Fprintf(&b, "%s%s%s\n", prefix, branch, name)
			count++
		}
		return nil
	}
	_ = walk(root, "")
	return strings.TrimRight(b.String(), "\n"), nil
}
