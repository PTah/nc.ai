package tools

import (
	"fmt"
	"strings"
)

// ApplySearchReplace replaces oldStr with newStr in content.
// If replaceAll is false, oldStr must occur exactly once.
func ApplySearchReplace(content, oldStr, newStr string, replaceAll bool) (string, int, error) {
	oldStr = normalizePatchNewlines(oldStr)
	newStr = normalizePatchNewlines(newStr)
	content = normalizePatchNewlines(content)
	if oldStr == "" {
		return "", 0, fmt.Errorf("old_string is empty")
	}
	count := strings.Count(content, oldStr)
	if count == 0 {
		return "", 0, fmt.Errorf("old_string not found in file (exact match required; re-read the file)")
	}
	if !replaceAll && count > 1 {
		return "", 0, fmt.Errorf("old_string found %d times; pass replace_all=true or include more surrounding context", count)
	}
	if replaceAll {
		return strings.ReplaceAll(content, oldStr, newStr), count, nil
	}
	return strings.Replace(content, oldStr, newStr, 1), 1, nil
}

// ParseSearchReplacePatch extracts old/new from a Cursor-style block:
//
//	<<<<<<< SEARCH
//	old
//	=======
//	new
//	>>>>>>> REPLACE
func ParseSearchReplacePatch(patch string) (oldStr, newStr string, err error) {
	patch = normalizePatchNewlines(strings.TrimSpace(patch))
	if patch == "" {
		return "", "", fmt.Errorf("patch is empty")
	}
	const (
		start = "<<<<<<< SEARCH"
		mid   = "======="
		end   = ">>>>>>> REPLACE"
	)
	si := strings.Index(patch, start)
	if si < 0 {
		return "", "", fmt.Errorf("patch missing %q marker", start)
	}
	afterStart := patch[si+len(start):]
	afterStart = strings.TrimPrefix(afterStart, "\n")
	mi := strings.Index(afterStart, "\n"+mid+"\n")
	sepLen := len("\n" + mid + "\n")
	if mi < 0 {
		mi = strings.Index(afterStart, "\n"+mid)
		sepLen = len("\n" + mid)
		if mi < 0 {
			mi = strings.Index(afterStart, mid)
			sepLen = len(mid)
			if mi < 0 {
				return "", "", fmt.Errorf("patch missing %q marker", mid)
			}
		}
	}
	oldStr = afterStart[:mi]
	rest := afterStart[mi+sepLen:]
	rest = strings.TrimPrefix(rest, "\n")
	ei := strings.Index(rest, "\n"+end)
	if ei < 0 {
		ei = strings.Index(rest, end)
		if ei < 0 {
			return "", "", fmt.Errorf("patch missing %q marker", end)
		}
		newStr = rest[:ei]
	} else {
		newStr = rest[:ei]
	}
	return oldStr, newStr, nil
}

func normalizePatchNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}
