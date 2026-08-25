package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/IHaveASegway/gitops/internal/format"
)

func (m Model) updateMenu(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.opIdx > 0 {
			m.opIdx--
		}
	case "down", "j":
		if m.opIdx < len(opDefs)-1 {
			m.opIdx++
		}
	case "home", "g":
		m.opIdx = 0
	case "end", "G":
		m.opIdx = len(opDefs) - 1
	case "r":
		m.loadRepos()
		m.flash = "Rescanned " + format.ShortenPath(m.baseDir)
		return m, waitInfo(m.infoCh, m.infoGen)
	case "enter", " ":
		op := m.op()
		if op.needsRepos && len(m.repos) == 0 {
			return m.flashf("No git repositories in %s — use init to clone an org, or start gitops with -d <dir>", format.ShortenPath(m.baseDir)), nil
		}
		if m.isInit() {
			m.openInput("")
			return m, textinput.Blink
		}
		m.enterRepoSelect()
	case "q":
		m.quit = true
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) renderMenu() string {
	sub := " · " + format.Plural(len(m.repos), "repo")
	if len(m.repos) > 0 {
		if m.infoLoading {
			sub += " · loading status…"
		} else {
			dirty, behind, ahead := m.repoSummary()
			if dirty > 0 {
				sub += fmt.Sprintf(" · %d dirty", dirty)
			}
			if behind > 0 {
				sub += fmt.Sprintf(" · %d behind", behind)
			}
			if ahead > 0 {
				sub += fmt.Sprintf(" · %d ahead", ahead)
			}
			if dirty+behind+ahead == 0 {
				sub += " · all clean"
			}
		}
	}
	sub = clipPathLeft(format.ShortenPath(m.baseDir), m.termWidth()-4-lipgloss.Width(sub)) + sub

	var b strings.Builder
	b.WriteString(m.header("", sub))
	w := m.termWidth() - 2
	for i, op := range opDefs {
		cursor := "    "
		name := padRight(op.name, 10)
		desc := dimStyle.Render(op.desc)
		switch {
		case i == m.opIdx:
			cursor = accentStyle.Render("  ▸ ")
			name = accentStyle.Render(name)
		case op.needsRepos && len(m.repos) == 0:
			name = dimStyle.Render(name)
		}
		if op.destructive {
			desc += warnStyle.Render("  !")
		}
		b.WriteString(clipStr(cursor+name+" "+desc, w) + "\n")
	}
	if len(m.repos) == 0 {
		b.WriteString("\n" + warnStyle.Render(clipStr("  No git repositories here — pick init to clone an org, or run gitops -d <dir>.", w)) + "\n")
	}
	b.WriteString(m.footer("↑/↓ move · enter select · r rescan · q quit"))
	return b.String()
}
