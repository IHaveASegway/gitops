package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/IHaveASegway/gitops/internal/clone"
	"github.com/IHaveASegway/gitops/internal/ops"
	"github.com/IHaveASegway/gitops/internal/runner"
)

// opFunc maps the selected menu entry to its operation.
func (m Model) opFunc() runner.Func {
	value := strings.TrimSpace(m.input.Value())
	switch m.op().name {
	case "pull":
		return ops.Pull
	case "sync":
		return ops.Sync
	case "reset":
		return ops.Reset
	case "branch":
		return ops.CreateBranch(value)
	case "push":
		return ops.Push(value)
	case "checkout":
		return ops.Checkout(value)
	default:
		return ops.Status
	}
}

func (m Model) startOpExec() (tea.Model, tea.Cmd) {
	var targets []string
	for i, it := range m.items {
		if m.selected[i] && it.repoIdx >= 0 {
			targets = append(targets, m.repos[it.repoIdx].path)
		}
	}
	return m.startExec(m.op().name, targets, m.opFunc())
}

func (m Model) startInitExec() (tea.Model, tea.Cmd) {
	var names []string
	byName := map[string]clone.Entry{}
	for i, it := range m.items {
		if m.selected[i] && it.planIdx >= 0 {
			e := m.plan.Entries[it.planIdx]
			names = append(names, e.Repo.Name)
			byName[e.Repo.Name] = e
		}
	}
	return m.startExec("init "+m.initOwner.Login, names, clone.Op(byName, m.initOpts))
}

// startExec runs fn over targets in the background and switches to the
// progress view. Events stream in through a channel.
func (m Model) startExec(title string, targets []string, fn runner.Func) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan runner.Event, 64)
	jobs := m.jobs
	go func() {
		runner.Run(ctx, targets, fn, jobs, func(ev runner.Event) { ch <- ev })
		close(ch)
	}()
	m.execTitle = title
	m.targets = targets
	m.states = make([]execState, len(targets))
	m.results = make([]runner.Result, len(targets))
	m.done = 0
	m.events = ch
	m.cancel = cancel
	m.cancelling, m.quitAfter = false, false
	m.flash = ""
	m.view = viewExec
	return m, tea.Batch(waitEvent(ch), m.spin.Tick)
}

func waitEvent(ch chan runner.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return execFinishedMsg{}
		}
		return execEventMsg(ev)
	}
}

func (m Model) updateExec(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case execEventMsg:
		if msg.Index < len(m.states) {
			if msg.Started {
				m.states[msg.Index] = stRunning
			} else {
				m.states[msg.Index] = stDone
				m.results[msg.Index] = msg.Result
				m.done++
			}
		}
		return m, waitEvent(m.events)
	case execFinishedMsg:
		if m.cancel != nil {
			m.cancel()
		}
		results := make([]runner.Result, len(m.results))
		copy(results, m.results)
		for i := range results {
			if m.states[i] != stDone {
				results[i] = runner.Result{Repo: filepath.Base(m.targets[i]), Error: "canceled"}
			}
		}
		m.results = results
		m.history = append(m.history, runRecord{title: m.execTitle, results: results})
		if m.quitAfter {
			m.quit = true
			return m, tea.Quit
		}
		m.view = viewResults
		m.doneCursor, m.doneScroll = 0, 0
		m.expanded = map[int]bool{}
		m.failOnly = false
		m.flash = ""
		return m, nil
	}
	return m, nil
}

func (m Model) targetNames() []string {
	names := make([]string, len(m.targets))
	for i, t := range m.targets {
		names[i] = filepath.Base(t)
	}
	return names
}

func (m Model) renderExec() string {
	tw := m.termWidth()
	total := len(m.targets)
	names := m.targetNames()
	nameW := m.nameWidth(names)
	detailW := max(4, tw-10-nameW)

	var b strings.Builder
	b.WriteString("\n" + titleStyle.Render(clipStr("  gitops › "+m.execTitle, tw-2)) + "\n")
	pct := 0.0
	if total > 0 {
		pct = float64(m.done) / float64(total)
	}
	fmt.Fprintf(&b, "  %s %d/%d\n\n", m.prog.ViewAs(pct), m.done, total)

	first := total
	for i, st := range m.states {
		if st != stDone {
			first = i
			break
		}
	}
	h := m.listHeight()
	start := min(max(0, first-2), max(0, total-h))
	end := min(total, start+h)
	if start > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↑ %d done", start)) + "\n")
	} else {
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		name := padRight(names[i], nameW)
		var line string
		switch m.states[i] {
		case stPending:
			line = dimStyle.Render("  · " + name)
		case stRunning:
			line = "  " + m.spin.View() + " " + name + "  " + dimStyle.Render("running…")
		default:
			r := m.results[i]
			if r.Success {
				line = "  " + successStyle.Render("✓") + " " + name + "  " + dimStyle.Render(clipStr(r.FirstLine(), detailW))
			} else {
				line = "  " + errStyle.Render("✗") + " " + name + "  " + errStyle.Render(clipStr(r.FirstLine(), detailW))
			}
		}
		b.WriteString(clipStr(line, tw-1) + "\n")
	}
	if end < total {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ↓ %d pending", total-end)) + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(m.footer("esc cancel · ctrl+c cancel and quit"))
	return b.String()
}
