// Package report prints results for the non-interactive CLI.
package report

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"

	"github.com/IHaveASegway/gitops/internal/runner"
)

var (
	// StdoutIsTTY reports whether stdout is an interactive terminal.
	StdoutIsTTY = isatty.IsTerminal(os.Stdout.Fd())
	// Color controls ANSI coloring; off when piped or when NO_COLOR is set.
	Color = StdoutIsTTY && os.Getenv("NO_COLOR") == ""
)

// Paint wraps s in the SGR code (e.g. "32" for green) when Color is on.
func Paint(code, s string) string {
	if !Color {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

// NameColumnWidth returns a column width that fits the longest name,
// bounded by minW and maxW.
func NameColumnWidth(names []string, minW, maxW int) int {
	w := minW
	for _, n := range names {
		if l := len([]rune(n)); l > w {
			w = l
		}
	}
	return min(w, maxW)
}

// PrintResults writes a report of results to w, multi-line outputs included.
func PrintResults(w io.Writer, title string, results []runner.Result) {
	names := make([]string, len(results))
	for i, r := range results {
		names[i] = r.Repo
	}
	nameW := NameColumnWidth(names, 12, 48)
	rule := strings.Repeat("─", nameW+36)

	fmt.Fprintf(w, "\n  %s\n  %s\n", Paint("1", title), rule)
	for _, r := range results {
		lines := strings.Split(r.Text(), "\n")
		if r.Success {
			fmt.Fprintf(w, "  %s %-*s  %s\n", Paint("32", "✓"), nameW, r.Repo, lines[0])
		} else {
			fmt.Fprintf(w, "  %s %-*s  %s\n", Paint("31", "✗"), nameW, r.Repo, Paint("31", lines[0]))
		}
		for _, l := range lines[1:] {
			fmt.Fprintf(w, "    %-*s  %s\n", nameW, "", Paint("2", l))
		}
	}
	ok, fail := runner.Summarize(results)
	fmt.Fprintf(w, "  %s\n  Total: %d  %s  %s\n\n", rule, len(results),
		Paint("32", fmt.Sprintf("✓ %d", ok)), Paint("31", fmt.Sprintf("✗ %d", fail)))
}
