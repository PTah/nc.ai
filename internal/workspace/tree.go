package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectTree builds a compact directory map (names only) for agent orientation.
// maxEntries caps listed files/dirs; deeper content is summarized as "…".
// In a multi-root workspace each root is shown as a top-level folder.
func (m *Manager) ProjectTree(maxEntries int) (string, error) {
	if maxEntries <= 0 {
		maxEntries = 400
	}
	entries := m.rootEntries()
	if len(entries) == 0 {
		return "", fmt.Errorf("no active project")
	}
	multi := len(entries) > 1
	var b strings.Builder
	count := 0

	walk := func(root, rootName, prefix string) error {
		var walkDir func(dir, prefix string) error
		walkDir = func(dir, prefix string) error {
			items, err := os.ReadDir(dir)
			if err != nil {
				return nil
			}
			var dirs, files []os.DirEntry
			for _, e := range items {
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
					if err := walkDir(filepath.Join(dir, name), nextPref); err != nil {
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
		_ = rootName
		return walkDir(root, prefix)
	}

	for i, e := range entries {
		last := i == len(entries)-1
		branch := "├── "
		prefix := "│   "
		if last {
			branch = "└── "
			prefix = "    "
		}
		if multi {
			fmt.Fprintf(&b, "%s%s/\n", branch, e.virt)
			count++
			if err := walk(e.root, e.virt, prefix); err != nil {
				if err == errSearchLimit {
					break
				}
			}
		} else {
			fmt.Fprintf(&b, "%s/\n", filepath.Base(e.root))
			_ = walk(e.root, e.virt, "")
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
