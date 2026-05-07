package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).MarginTop(1)
)

type tuiView int

const (
	viewOps tuiView = iota
	viewRepos
	viewInput
	viewExec
	viewDone
)

type opDef struct {
	name     string
	desc     string
	hasInput bool
	label    string
	prompt   string
}

var opDefs = []opDef{
	{"pull", "Checkout default branch and pull latest", false, "", ""},
	{"sync", "Stash changes, pull latest, pop stash", false, "", ""},
	{"reset", "Discard changes, force checkout default, pull", false, "", ""},
	{"branch", "Create new branch from default", true, "Branch name", "feature/my-branch"},
	{"push", "Stage all, commit and push current branch", true, "Commit message", "fix: update something"},
	{"checkout", "Checkout an existing branch", true, "Branch name", "feature/existing-branch"},
	{"status", "Show status of all repositories", false, "", ""},
}

type opResultMsg struct {
	result Result
}

type tuiModel struct {
	view    tuiView
	baseDir string
	repos   []string
	names   []string

	opIdx int

	repoIdx   int
	repoSel   []bool
	selectAll bool

	input textinput.Model

	spin    spinner.Model
	results []Result
	total   int

	scrollOff int

	width  int
	height int

	quit bool
}

func newTUIModel(baseDir string, repos []string) tuiModel {
	names := make([]string, len(repos))
	for i, r := range repos {
		if rel, err := filepath.Rel(baseDir, r); err == nil {
			names[i] = rel
		} else {
			names[i] = filepath.Base(r)
		}
	}

	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 50
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	return tuiModel{
		view:      viewOps,
		baseDir:   baseDir,
		repos:     repos,
		names:     names,
		repoSel:   make([]bool, len(repos)),
		selectAll: true,
		input:     ti,
		spin:      sp,
	}
}

func (m tuiModel) Init() tea.Cmd {
	return m.spin.Tick
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quit = true
			return m, tea.Quit
		}
	}

	switch m.view {
	case viewOps:
		return m.updateOps(msg)
	case viewRepos:
		return m.updateRepos(msg)
	case viewInput:
		return m.updateInput(msg)
	case viewExec:
		return m.updateExec(msg)
	case viewDone:
		return m.updateDone(msg)
	}
	return m, nil
}

func (m tuiModel) updateOps(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "up", "k":
			if m.opIdx > 0 {
				m.opIdx--
			}
		case "down", "j":
			if m.opIdx < len(opDefs)-1 {
				m.opIdx++
			}
		case "enter":
			m.view = viewRepos
			m.repoIdx = 0
		case "q":
			m.quit = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m tuiModel) updateRepos(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		totalItems := len(m.names) + 1
		switch msg.String() {
		case "up", "k":
			if m.repoIdx > 0 {
				m.repoIdx--
			}
		case "down", "j":
			if m.repoIdx < totalItems-1 {
				m.repoIdx++
			}
		case " ":
			if m.repoIdx == 0 {
				m.selectAll = !m.selectAll
				for i := range m.repoSel {
					m.repoSel[i] = false
				}
			} else {
				m.repoSel[m.repoIdx-1] = !m.repoSel[m.repoIdx-1]
				m.selectAll = false
			}
		case "enter":
			op := opDefs[m.opIdx]
			if op.hasInput {
				m.input.SetValue("")
				m.input.Placeholder = op.prompt
				m.view = viewInput
				return m, textinput.Blink
			}
			return m.startExec()
		case "esc", "q":
			m.view = viewOps
		}
	}
	return m, nil
}

func (m tuiModel) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "enter":
			if strings.TrimSpace(m.input.Value()) != "" {
				return m.startExec()
			}
			return m, nil
		case "esc":
			m.view = viewRepos
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m tuiModel) updateExec(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case opResultMsg:
		m.results = append(m.results, msg.result)
		if len(m.results) >= m.total {
			m.view = viewDone
			m.scrollOff = 0
			return m, nil
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m tuiModel) updateDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		maxVis := m.visibleLines()
		switch msg.String() {
		case "up", "k":
			if m.scrollOff > 0 {
				m.scrollOff--
			}
		case "down", "j":
			maxOff := len(m.results) - maxVis
			if maxOff < 0 {
				maxOff = 0
			}
			if m.scrollOff < maxOff {
				m.scrollOff++
			}
		case "q", "enter":
			m.quit = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m tuiModel) selectedRepos() []string {
	if m.selectAll {
		return m.repos
	}
	var sel []string
	for i, on := range m.repoSel {
		if on {
			sel = append(sel, m.repos[i])
		}
	}
	if len(sel) == 0 {
		return m.repos
	}
	return sel
}

func (m tuiModel) opFunc() func(string) Result {
	switch opDefs[m.opIdx].name {
	case "pull":
		return opPull
	case "sync":
		return opSync
	case "reset":
		return opReset
	case "branch":
		return opCreateBranch(m.input.Value())
	case "push":
		return opPush(m.input.Value())
	case "checkout":
		return opCheckout(m.input.Value())
	default:
		return opStatus
	}
}

func (m tuiModel) startExec() (tea.Model, tea.Cmd) {
	repos := m.selectedRepos()
	m.view = viewExec
	m.total = len(repos)
	m.results = nil

	fn := m.opFunc()
	cmds := make([]tea.Cmd, len(repos))
	for i, r := range repos {
		path := r
		baseDir := m.baseDir
		cmds[i] = func() tea.Msg {
			result := fn(path)
			if rel, err := filepath.Rel(baseDir, path); err == nil {
				result.Repo = rel
			}
			return opResultMsg{result: result}
		}
	}
	return m, tea.Batch(append(cmds, m.spin.Tick)...)
}

func (m tuiModel) View() string {
	if m.quit {
		return ""
	}
	switch m.view {
	case viewOps:
		return m.renderOps()
	case viewRepos:
		return m.renderRepos()
	case viewInput:
		return m.renderInput()
	case viewExec:
		return m.renderExec()
	case viewDone:
		return m.renderDone()
	}
	return ""
}

func (m tuiModel) renderOps() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("  gitops"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Mass git operations across repositories"))
	b.WriteString("\n\n")

	for i, op := range opDefs {
		cursor := "    "
		nameStyle := lipgloss.NewStyle()
		if i == m.opIdx {
			cursor = accentStyle.Render("  > ")
			nameStyle = accentStyle
		}
		b.WriteString(fmt.Sprintf("%s%-12s %s\n", cursor, nameStyle.Render(op.name), dimStyle.Render(op.desc)))
	}

	b.WriteString(helpStyle.Render("\n  up/down navigate | enter select | q quit"))
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) renderRepos() string {
	var b strings.Builder
	op := opDefs[m.opIdx]
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf("  gitops > %s", op.name)))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("  Select repositories"))
	b.WriteString("\n\n")

	totalItems := len(m.names) + 1
	maxVis := m.visibleLines()
	start, end := scrollWindow(m.repoIdx, totalItems, maxVis)
	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more above\n", start)))
	}

	for i := start; i < end; i++ {
		cursor := "    "
		if i == m.repoIdx {
			cursor = accentStyle.Render("  > ")
		}
		if i == 0 {
			marker := "[ ]"
			label := "All repositories"
			if m.selectAll {
				marker = accentStyle.Render("[x]")
			}
			style := lipgloss.NewStyle()
			if i == m.repoIdx {
				style = accentStyle
			}
			b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, marker, style.Render(label)))
		} else {
			idx := i - 1
			marker := "[ ]"
			if m.repoSel[idx] {
				marker = accentStyle.Render("[x]")
			}
			style := lipgloss.NewStyle()
			if i == m.repoIdx {
				style = accentStyle
			}
			b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, marker, style.Render(m.names[idx])))
		}
	}

	if end < totalItems {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more below\n", totalItems-end)))
	}

	count := m.selectedCount()
	b.WriteString(dimStyle.Render(fmt.Sprintf("\n  %d selected", count)))
	b.WriteString(helpStyle.Render("\n  up/down navigate | space toggle | enter confirm | esc back"))
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) renderInput() string {
	var b strings.Builder
	op := opDefs[m.opIdx]
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf("  gitops > %s", op.name)))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(fmt.Sprintf("  %s", op.label)))
	b.WriteString("\n\n  ")
	b.WriteString(m.input.View())
	b.WriteString(helpStyle.Render("\n\n  enter confirm | esc back"))
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) renderExec() string {
	var b strings.Builder
	op := opDefs[m.opIdx]
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf("  gitops > %s", op.name)))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s Running %d/%d\n\n", m.spin.View(), len(m.results), m.total))

	maxVis := m.visibleLines()
	start := 0
	if len(m.results) > maxVis {
		start = len(m.results) - maxVis
	}

	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d completed above\n", start)))
	}

	for _, r := range m.results[start:] {
		if r.Success {
			b.WriteString(fmt.Sprintf("  %s %-28s %s\n",
				successStyle.Render("+"), r.Repo, dimStyle.Render(clip(r.Output, 45))))
		} else {
			b.WriteString(fmt.Sprintf("  %s %-28s %s\n",
				errStyle.Render("x"), r.Repo, errStyle.Render(clip(r.Error, 45))))
		}
	}

	pending := m.total - len(m.results)
	if pending > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n  %s %d remaining...\n", m.spin.View(), pending)))
	}

	return b.String()
}

func (m tuiModel) renderDone() string {
	var b strings.Builder
	op := opDefs[m.opIdx]
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(fmt.Sprintf("  gitops > %s | done", op.name)))
	b.WriteString("\n\n")

	ok, fail := 0, 0
	for _, r := range m.results {
		if r.Success {
			ok++
		} else {
			fail++
		}
	}

	maxVis := m.visibleLines()
	start := m.scrollOff
	end := start + maxVis
	if end > len(m.results) {
		end = len(m.results)
	}

	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more above\n", start)))
	}

	for _, r := range m.results[start:end] {
		if r.Success {
			out := r.Output
			if out == "" {
				out = "ok"
			}
			b.WriteString(fmt.Sprintf("  %s %-28s %s\n",
				successStyle.Render("+"), r.Repo, dimStyle.Render(clip(out, 45))))
		} else {
			b.WriteString(fmt.Sprintf("  %s %-28s %s\n",
				errStyle.Render("x"), r.Repo, errStyle.Render(clip(r.Error, 45))))
		}
	}

	if end < len(m.results) {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more below\n", len(m.results)-end)))
	}

	b.WriteString(fmt.Sprintf("\n  Total: %d  ", len(m.results)))
	b.WriteString(successStyle.Render(fmt.Sprintf("+ %d  ", ok)))
	if fail > 0 {
		b.WriteString(errStyle.Render(fmt.Sprintf("x %d", fail)))
	} else {
		b.WriteString(dimStyle.Render("x 0"))
	}

	scrollHint := ""
	if len(m.results) > maxVis {
		scrollHint = " | up/down scroll"
	}
	b.WriteString(helpStyle.Render(fmt.Sprintf("\n\n  q/enter exit%s", scrollHint)))
	b.WriteString("\n")
	return b.String()
}

func (m tuiModel) visibleLines() int {
	v := m.height - 10
	if v < 8 {
		v = 8
	}
	return v
}

func (m tuiModel) selectedCount() int {
	if m.selectAll {
		return len(m.repos)
	}
	n := 0
	for _, on := range m.repoSel {
		if on {
			n++
		}
	}
	if n == 0 {
		return len(m.repos)
	}
	return n
}

func scrollWindow(cursor, total, maxVis int) (int, int) {
	if total <= maxVis {
		return 0, total
	}
	start := cursor - maxVis/2
	if start < 0 {
		start = 0
	}
	end := start + maxVis
	if end > total {
		end = total
		start = end - maxVis
	}
	return start, end
}

func clip(s string, n int) string {
	s = strings.SplitN(s, "\n", 2)[0]
	if len(s) > n {
		return s[:n-1] + "..."
	}
	return s
}
