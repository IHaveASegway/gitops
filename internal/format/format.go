// Package format holds small text helpers shared by the CLI and the TUI.
package format

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Plural renders "1 repo" / "3 repos" / "2 repositories".
func Plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	if strings.HasSuffix(word, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(word, "y"))
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// JoinNames renders up to limit names, then "… +N" for the rest.
func JoinNames(names []string, limit int) string {
	if len(names) <= limit {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:limit], ", ") + fmt.Sprintf(", … +%d", len(names)-limit)
}

// ShortenPath replaces the home directory prefix with "~" for display.
func ShortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}
