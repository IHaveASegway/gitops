package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/IHaveASegway/gitops/internal/clone"
)

// enterRepoSelect fills the picker with the discovered repositories.
func (m *Model) enterRepoSelect() {
	m.items = make([]listItem, len(m.repos))
	m.selected = make([]bool, len(m.repos))
	for i, r := range m.repos {
		m.items[i] = listItem{name: r.name, repoIdx: i, planIdx: -1, selectable: true}
		if m.preselect != nil {
			m.selected[i] = m.preselect[strings.ToLower(r.name)]
		} else {
			m.selected[i] = true
		}
	}
	m.preselect = nil // only honored for the first selection
	m.resetList()
}

// enterPlanSelect fills the picker with the init plan; clonable repos start selected.
func (m *Model) enterPlanSelect() {
	m.items, m.selected = nil, nil
	for i, e := range m.plan.Entries {
		if e.Action == clone.ActionFiltered {
			continue
		}
		m.items = append(m.items, listItem{name: e.Repo.Name, repoIdx: -1, planIdx: i, selectable: e.Action == clone.ActionClone})
		m.selected = append(m.selected, e.Action == clone.ActionClone)
	}
	m.resetList()
}

func (m *Model) resetList() {
	m.filter.SetValue("")
	m.filter.Blur()
	m.filtering = false
	m.cursor, m.scroll = 0, 0
	m.applyFilter()
	m.view = viewPicker
}

func (m *Model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.visible = m.visible[:0]
	for i, it := range m.items {
		if q == "" || strings.Contains(strings.ToLower(it.name), q) {
			m.visible = append(m.visible, i)
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = max(0, len(m.visible)-1)
	}
	m.clampScroll()
}

func (m *Model) clampScroll() {
	h := m.listHeight()
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+h {
		m.scroll = m.cursor - h + 1
	}
	m.scroll = max(0, m.scroll)
}

func (m *Model) moveCursor(delta int) {
	n := len(m.visible)
	if n == 0 {
		m.cursor = 0
		return
	}
	m.cursor = min(max(0, m.cursor+delta), n-1)
	m.clampScroll()
}

func (m Model) selectedCount() int {
	n := 0
	for _, s := range m.selected {
		if s {
			n++
		}
	}
	return n
}

func (m Model) selectedNames() []string {
	var names []string
	for i, it := range m.items {
		if m.selected[i] {
			names = append(names, it.name)
		}
	}
	return names
}

// setVisible applies fn to every selectable row that passes the filter.
func (m *Model) setVisible(fn func(cur bool) bool) {
	for _, i := range m.visible {
		if m.items[i].selectable {
			m.selected[i] = fn(m.selected[i])
		}
	}
}

func (m Model) updatePicker(key string) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch key {
		case "enter":
			m.filtering = false
			m.filter.Blur()
		case "esc":
			m.filtering = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.applyFilter()
		case "up":
			m.moveCursor(-1)
		case "down":
			m.moveCursor(1)
		case "pgup":
			m.moveCursor(-m.listHeight())
		case "pgdown":
			m.moveCursor(m.listHeight())
		}
		return m, nil
	}
	switch key {
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.listHeight())
	case "pgdown":
		m.moveCursor(m.listHeight())
	case "home", "g":
		m.moveCursor(-len(m.visible))
	case "end", "G":
		m.moveCursor(len(m.visible))
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case " ":
		if len(m.visible) > 0 {
			i := m.visible[m.cursor]
			if m.items[i].selectable {
				m.selected[i] = !m.selected[i]
			}
		}
	case "a":
		m.setVisible(func(bool) bool { return true })
	case "n":
		m.setVisible(func(bool) bool { return false })
	case "i":
		m.setVisible(func(cur bool) bool { return !cur })
	case "enter":
		return m.afterSelect()
	case "esc", "q":
		if key == "esc" && m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
			return m, nil
		}
		if m.isInit() {
			m.openInput(m.input.Value())
			return m, textinput.Blink
		}
		m.view = viewMenu
	}
	return m, nil
}

// afterSelect moves on from the picker: confirmation, text input or execution.
func (m Model) afterSelect() (tea.Model, tea.Cmd) {
	if m.selectedCount() == 0 {
		m.flash = "Select at least one repository (space toggles, a selects all)"
		return m, nil
	}
	op := m.op()
	switch {
	case m.isInit(), op.destructive && op.input == "":
		m.view = viewConfirm
		return m, nil
	case op.input != "":
		m.openInput("")
		return m, textinput.Blink
	}
	return m.startOpExec()
}

// rowDetail renders the right-hand column of a picker row.
func (m Model) rowDetail(it listItem, w int) string {
	if it.repoIdx >= 0 {
		r := m.repos[it.repoIdx]
		if !r.loaded {
			return dimStyle.Render("…")
		}
		if r.info.Err != "" {
			return errStyle.Render(clipStr(r.info.Err, w))
		}
		s := dimStyle.Render(r.info.Tag())
		if r.info.Dirty > 0 {
			s += " " + warnStyle.Render(fmt.Sprintf("*%d", r.info.Dirty))
		}
		return clipStr(s, w)
	}
	if it.planIdx >= 0 && m.plan != nil {
		e := m.plan.Entries[it.planIdx]
		switch e.Action {
		case clone.ActionClone:
			s := successStyle.Render("clone")
			if e.Repo.Private {
				s += dimStyle.Render(" · private")
			}
			if e.Repo.Fork {
				s += dimStyle.Render(" · fork")
			}
			return clipStr(s, w)
		case clone.ActionExists:
			s := "already present"
			if e.Reason != "" {
				s += " " + e.Reason
			}
			return dimStyle.Render(clipStr(s, w))
		case clone.ActionArchived:
			return dimStyle.Render("archived · skipped")
		case clone.ActionFork:
			return dimStyle.Render("fork · skipped")
		case clone.ActionConflict:
			return errStyle.Render(clipStr(e.Reason, w))
		}
	}
	return ""
}

func (m Model) renderPicker() string {
	var title, sub string
	if m.isInit() && m.plan != nil {
		title = "init › " + m.initOwner.Login
		sub = m.plan.SelectionLine(m.selectedCount())
	} else {
		title = m.op().name
		sub = fmt.Sprintf("Select repositories · %d of %d selected", m.selectedCount(), len(m.items))
	}
	if q := m.filter.Value(); q != "" && !m.filtering {
		sub += fmt.Sprintf(" · filter %q (%d shown)", q, len(m.visible))
	}

	var b strings.Builder
	b.WriteString(m.header(title, sub))
	if m.filtering {
		b.WriteString("  " + m.filter.View() + "\n")
	}

	tw := m.termWidth()
	names := make([]string, 0, len(m.visible))
	for _, i := range m.visible {
		names = append(names, m.items[i].name)
	}
	nameW := m.nameWidth(names)
	detailW := max(4, tw-12-nameW)

	h := m.listHeight()
	start := min(m.scroll, max(0, len(m.visible)-1))
	end := min(len(m.visible), start+h)
	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	} else {
		b.WriteString("\n")
	}
	for row := start; row < end; row++ {
		idx := m.visible[row]
		it := m.items[idx]
		cur := row == m.cursor
		cursor := "  "
		if cur {
			cursor = accentStyle.Render("▸ ")
		}
		box := "[ ]"
		switch {
		case !it.selectable:
			box = dimStyle.Render("[-]")
		case m.selected[idx]:
			box = accentStyle.Render("[x]")
		}
		name := padRight(it.name, nameW)
		switch {
		case cur:
			name = accentStyle.Render(name)
		case !it.selectable:
			name = dimStyle.Render(name)
		}
		b.WriteString(clipStr("  "+cursor+box+" "+name+"  "+m.rowDetail(it, detailW), tw-1) + "\n")
	}
	if len(m.visible) == 0 {
		b.WriteString(dimStyle.Render("  (no repositories match the filter)") + "\n")
	}
	if end < len(m.visible) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d more", len(m.visible)-end)) + "\n")
	} else {
		b.WriteString("\n")
	}

	help := "space toggle · a all · n none · i invert · / filter · enter continue · esc back"
	if m.filtering {
		help = "type to filter · ↑/↓ move · enter apply · esc clear"
	}
	b.WriteString(m.footer(help))
	return b.String()
}
