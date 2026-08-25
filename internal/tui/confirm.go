package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/IHaveASegway/gitops/internal/format"
)

func (m Model) updateConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		return m.confirmed()
	case "enter":
		// With duplicate warnings an accidental Enter must not clone.
		if m.isInit() && m.plan.HasWarnings() {
			return m, nil
		}
		return m.confirmed()
	case "h":
		if m.isInit() && !m.plan.Here {
			m.rebuildPlan(m.plan.BaseDir, true)
			m.flash = "Plan rebuilt: cloning directly into " + format.ShortenPath(m.plan.TargetDir)
		}
	case "u":
		if m.isInit() {
			for _, w := range m.plan.Warnings {
				if !w.Nested {
					m.rebuildPlan(w.Dir, true)
					m.flash = "Plan rebuilt: adding missing repos to " + format.ShortenPath(w.Dir)
					break
				}
			}
		}
	case "n", "esc", "q":
		if m.op().input != "" && !m.isInit() {
			m.openInput(m.input.Value())
			return m, textinput.Blink
		}
		m.view = viewPicker
	}
	return m, nil
}

func (m Model) confirmed() (tea.Model, tea.Cmd) {
	if m.isInit() {
		return m.startInitExec()
	}
	return m.startOpExec()
}

func (m Model) renderConfirm() string {
	tw := m.termWidth()
	var b strings.Builder
	if m.isInit() && m.plan != nil {
		p := m.plan
		b.WriteString(m.header("init › "+p.Owner.Login, "Review before cloning"))
		kind := "Organization"
		if !p.Owner.IsOrg() {
			kind = "User"
		}
		auth := "no token — public repos only"
		if m.initOpts.Token != "" {
			auth = "token from " + m.tokenSrc
		}
		rows := [][2]string{
			{kind, p.Owner.Login + "  " + p.OwnerURL()},
			{"Target", clipPathLeft(format.ShortenPath(p.TargetDir), tw-16)},
			{"Protocol", m.initOpts.Protocol + " · " + auth},
			{"Repos", p.SelectionLine(m.selectedCount())},
		}
		for _, r := range rows {
			b.WriteString("  " + dimStyle.Render(padRight(r[0], 13)) + clipStr(r[1], tw-16) + "\n")
		}
		hasNested, hasOther := false, false
		for _, w := range p.Warnings {
			b.WriteString("\n")
			for i, line := range p.WarningLines(w) {
				if i == 0 {
					wrapLines(&b, line, tw-6, warnStyle.Render("  ⚠ "))
				} else {
					wrapLines(&b, line, tw-6, "    ")
				}
			}
			if w.Nested {
				hasNested = true
			} else {
				hasOther = true
			}
		}
		help := "y clone · esc back"
		if p.HasWarnings() {
			help = "y clone anyway · esc back"
			if hasNested {
				help += " · h clone into this directory instead"
			}
			if hasOther {
				help += " · u add to the existing checkout instead"
			}
		}
		b.WriteString(m.footer(help))
		return b.String()
	}

	op := m.op()
	names := m.selectedNames()
	b.WriteString(m.header(op.name, "Confirm"))
	var text string
	switch op.name {
	case "reset":
		text = fmt.Sprintf("reset will permanently discard all uncommitted changes and untracked files in %s, then force-checkout the default branch and pull.", format.Plural(len(names), "repository"))
	case "push":
		text = fmt.Sprintf("push will run git add -A, commit %q and push the current branch in %s.", strings.TrimSpace(m.input.Value()), format.Plural(len(names), "repository"))
	default:
		text = fmt.Sprintf("Run %s in %s.", op.name, format.Plural(len(names), "repository"))
	}
	wrapLines(&b, text, tw-6, warnStyle.Render("  ⚠ "))
	b.WriteString("\n")
	wrapLines(&b, dimStyle.Render(format.JoinNames(names, 15)), tw-6, "    ")
	b.WriteString(m.footer("y confirm · esc back"))
	return b.String()
}
