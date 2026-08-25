package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/IHaveASegway/gitops/internal/format"
	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/runner"
)

// doneRows returns the result indices shown in the results view.
func (m Model) doneRows() []int {
	var rows []int
	for i, r := range m.results {
		if !m.failOnly || !r.Success {
			rows = append(rows, i)
		}
	}
	return rows
}

func (m Model) updateResults(key string) (tea.Model, tea.Cmd) {
	rows := m.doneRows()
	move := func(delta int) {
		if len(rows) == 0 {
			m.doneCursor = 0
			return
		}
		m.doneCursor = min(max(0, m.doneCursor+delta), len(rows)-1)
	}
	switch key {
	case "up", "k":
		move(-1)
	case "down", "j":
		move(1)
	case "pgup":
		move(-m.listHeight())
	case "pgdown":
		move(m.listHeight())
	case "home", "g":
		move(-len(rows))
	case "end", "G":
		move(len(rows))
	case "enter", " ", "tab":
		if len(rows) > 0 {
			i := rows[m.doneCursor]
			m.expanded[i] = !m.expanded[i]
		}
	case "e":
		all := len(m.expanded) < len(m.results)
		m.expanded = map[int]bool{}
		if all {
			for i := range m.results {
				m.expanded[i] = true
			}
		}
	case "f":
		m.failOnly = !m.failOnly
		m.doneCursor, m.doneScroll = 0, 0
	case "esc", "backspace":
		return m.backToMenu()
	case "q":
		m.quit = true
		return m, tea.Quit
	}
	m.fixDoneScroll()
	return m, nil
}

// backToMenu returns to the menu, refreshing repo state. After a successful
// init the TUI moves into the freshly cloned org directory.
func (m Model) backToMenu() (tea.Model, tea.Cmd) {
	if m.isInit() && m.plan != nil && m.plan.TargetDir != m.baseDir {
		if repos, err := git.Discover(m.plan.TargetDir); err == nil && len(repos) > 0 {
			m.baseDir = m.plan.TargetDir
			m.flash = "Now operating on " + format.ShortenPath(m.baseDir)
		}
	}
	m.loadRepos()
	m.view = viewMenu
	return m, waitInfo(m.infoCh, m.infoGen)
}

func (m Model) doneNameWidth() int {
	names := make([]string, len(m.results))
	for i, r := range m.results {
		names[i] = r.Repo
	}
	return m.nameWidth(names)
}

func (m Model) doneDetailWidth() int {
	return max(4, m.termWidth()-10-m.doneNameWidth())
}

// doneExtraLines returns the plain, wrapped lines shown under an expanded row.
func (m Model) doneExtraLines(i int) []string {
	if !m.expanded[i] {
		return nil
	}
	lines := strings.Split(m.results[i].Text(), "\n")
	if len(lines) <= 1 {
		return []string{"(no further output)"}
	}
	w := m.doneDetailWidth()
	var out []string
	for _, l := range lines[1:] {
		out = append(out, strings.Split(ansi.Hardwrap(l, w, false), "\n")...)
	}
	return out
}

// fixDoneScroll keeps the cursor row within the visible window.
func (m *Model) fixDoneScroll() {
	rows := m.doneRows()
	h := m.listHeight()
	line, start, end := 0, 0, 0
	for r, i := range rows {
		n := 1 + len(m.doneExtraLines(i))
		if r == m.doneCursor {
			start, end = line, line+n
			break
		}
		line += n
	}
	if start < m.doneScroll {
		m.doneScroll = start
	}
	if end > m.doneScroll+h {
		m.doneScroll = end - h
	}
	m.doneScroll = max(0, min(m.doneScroll, start))
}

func (m Model) renderResults() string {
	tw := m.termWidth()
	ok, fail := runner.Summarize(m.results)
	sub := fmt.Sprintf("Total %d · ✓ %d · ✗ %d", len(m.results), ok, fail)
	if m.failOnly {
		sub += " · showing failures only"
	}
	nameW := m.doneNameWidth()
	detailW := m.doneDetailWidth()

	var lines []string
	for r, i := range m.doneRows() {
		res := m.results[i]
		cur := r == m.doneCursor
		cursor := "  "
		if cur {
			cursor = accentStyle.Render("▸ ")
		}
		name := padRight(res.Repo, nameW)
		if cur {
			name = accentStyle.Render(name)
		}
		mark, detail := successStyle.Render("✓"), dimStyle.Render(clipStr(res.FirstLine(), detailW))
		if !res.Success {
			mark, detail = errStyle.Render("✗"), errStyle.Render(clipStr(res.FirstLine(), detailW))
		}
		lines = append(lines, clipStr("  "+cursor+mark+" "+name+"  "+detail, tw-1))
		for _, extra := range m.doneExtraLines(i) {
			style := dimStyle
			if !res.Success {
				style = errStyle
			}
			lines = append(lines, "      "+style.Render(extra))
		}
	}

	var b strings.Builder
	b.WriteString(m.header(m.execTitle+" · done", sub))
	h := m.listHeight()
	start := min(m.doneScroll, max(0, len(lines)-1))
	end := min(len(lines), start+h)
	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	} else {
		b.WriteString("\n")
	}
	for _, l := range lines[start:end] {
		b.WriteString(l + "\n")
	}
	if len(lines) == 0 {
		b.WriteString(dimStyle.Render("  (no failures)") + "\n")
	}
	if end < len(lines) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(lines)-end)) + "\n")
	} else {
		b.WriteString("\n")
	}
	help := "↑/↓ move · enter expand · e expand all · f failures only · esc menu · q quit"
	if m.isInit() {
		help = "↑/↓ move · enter expand · e expand all · f failures only · esc open org dir · q quit"
	}
	b.WriteString(m.footer(help))
	return b.String()
}
