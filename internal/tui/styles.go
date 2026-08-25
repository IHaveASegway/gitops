package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	spinnerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
)

// clipStr truncates the first line of s to w cells, ANSI- and rune-aware.
func clipStr(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s, _, _ = strings.Cut(s, "\n")
	return ansi.Truncate(s, w, "…")
}

// clipPathLeft shortens a path from the left ("…/GitHub/acme") to fit w cells.
func clipPathLeft(p string, w int) string {
	if w <= 1 {
		return ""
	}
	if pw := lipgloss.Width(p); pw > w {
		return "…" + ansi.TruncateLeft(p, pw-w+1, "")
	}
	return p
}

// padRight pads s with spaces to w cells; measure before styling.
func padRight(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// wrapLines word-wraps text to w cells; the first line gets prefix, the
// following lines are indented to align with it.
func wrapLines(b *strings.Builder, text string, w int, prefix string) {
	for i, line := range strings.Split(ansi.Wordwrap(text, max(10, w), ""), "\n") {
		if i == 0 {
			b.WriteString(prefix + line + "\n")
		} else {
			b.WriteString("    " + line + "\n")
		}
	}
}

func (m Model) termWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

// header renders the two-line title block every view starts with.
func (m Model) header(title, subtitle string) string {
	w := m.termWidth() - 2
	if title != "" {
		title = " › " + title
	}
	return "\n" + titleStyle.Render(clipStr("  gitops"+title, w)) + "\n" +
		dimStyle.Render(clipStr("  "+subtitle, w)) + "\n\n"
}

// footer renders the flash message line and the key help line.
func (m Model) footer(help string) string {
	w := m.termWidth() - 2
	flash := ""
	if m.flash != "" {
		flash = warnStyle.Render(clipStr("  "+m.flash, w))
	}
	return "\n" + flash + "\n" + dimStyle.Render(clipStr("  "+help, w)) + "\n"
}

// nameWidth returns the name column width for a list of names.
func (m Model) nameWidth(names []string) int {
	w := 8
	for _, n := range names {
		w = max(w, lipgloss.Width(n))
	}
	return min(w, max(8, (m.termWidth()-24)/2))
}
