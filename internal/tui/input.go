package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/IHaveASegway/gitops/internal/clone"
	"github.com/IHaveASegway/gitops/internal/format"
	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/github"
)

// openInput shows the text prompt of the current op with an initial value.
func (m *Model) openInput(value string) {
	op := m.op()
	m.input.SetValue(value)
	m.input.Placeholder = op.placeholder
	m.input.CursorEnd()
	m.input.Focus()
	m.inputErr = ""
	m.view = viewInput
}

func (m Model) updateInput(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.inputErr = ""
		if m.isInit() {
			m.view = viewMenu
		} else {
			m.view = viewPicker
		}
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if value == "" {
			m.inputErr = m.op().input + " is required"
			return m, nil
		}
		switch m.op().name {
		case "branch", "checkout":
			if err := git.CheckBranchName(value); err != nil {
				m.inputErr = err.Error()
				return m, nil
			}
		case "init":
			ref, err := github.ParseOwner(value)
			if err != nil {
				m.inputErr = err.Error()
				return m, nil
			}
			m.ownerRef = ref
			m.initGen++
			m.view = viewLoading
			return m, tea.Batch(loadInit(ref, m.initGen), m.spin.Tick)
		}
		if m.op().destructive {
			m.view = viewConfirm
			return m, nil
		}
		return m.startOpExec()
	}
	return m, nil
}

func (m Model) renderInput() string {
	op := m.op()
	w := m.termWidth() - 4
	var b strings.Builder
	b.WriteString(m.header(op.name, op.input))
	b.WriteString("  " + m.input.View() + "\n")
	if m.inputErr != "" {
		b.WriteString("\n" + errStyle.Render(clipStr("  ✗ "+m.inputErr, w)) + "\n")
	}
	var hints []string
	switch op.name {
	case "init":
		hints = []string{
			"Accepts https://github.com/<org>, github.com/<org> or just <org> (users work too).",
			"Repos are cloned into " + format.ShortenPath(m.baseDir) + "/<org>/ — existing checkouts are detected first.",
			"Private repos need a token: GH_TOKEN, GITHUB_TOKEN or gh auth login.",
		}
	case "push":
		hints = []string{"Stages all changes except .DS_Store, commits with <message> and pushes each selected repo's current branch."}
	case "branch":
		hints = []string{"Each repo checks out its default branch, pulls, then creates the branch from there."}
	}
	if len(hints) > 0 {
		b.WriteString("\n")
		for _, h := range hints {
			b.WriteString(dimStyle.Render(clipStr("  "+h, w)) + "\n")
		}
	}
	b.WriteString(m.footer("enter confirm · esc back"))
	return b.String()
}

// loadInit resolves the org and lists its repositories in the background.
func loadInit(ref github.OwnerRef, gen int) tea.Cmd {
	return func() tea.Msg {
		token, src := github.FindToken(ref.Host)
		client := github.NewClient(ref.Host, token)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		owner, err := client.LookupOwner(ctx, ref.Owner)
		if err != nil {
			return initLoadedMsg{gen: gen, err: err}
		}
		repos, err := client.ListRepos(ctx, owner, nil)
		if err != nil {
			return initLoadedMsg{gen: gen, err: err}
		}
		opts := clone.Options{Host: ref.Host, Protocol: github.DefaultProtocol(ref.Host), Token: token}
		return initLoadedMsg{gen: gen, owner: owner, repos: repos, opts: opts, tokenSrc: src}
	}
}

func (m Model) handleInitLoaded(msg initLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.initGen || m.view != viewLoading {
		return m, nil
	}
	if msg.err != nil {
		m.inputErr = msg.err.Error()
		m.view = viewInput
		return m, textinput.Blink
	}
	if len(msg.repos) == 0 {
		m.inputErr = fmt.Sprintf("%s has no repositories visible to you", msg.owner.Login)
		m.view = viewInput
		return m, textinput.Blink
	}
	m.initOwner, m.initRepos, m.initOpts, m.tokenSrc = msg.owner, msg.repos, msg.opts, msg.tokenSrc
	m.rebuildPlan(m.baseDir, false)
	return m, nil
}

// rebuildPlan (re)computes the init plan for a base directory and returns
// to the picker with clonable repos preselected.
func (m *Model) rebuildPlan(baseDir string, here bool) {
	m.plan = clone.BuildPlan(clone.Request{
		BaseDir: baseDir, Here: here, Host: m.ownerRef.Host, Owner: m.initOwner, Repos: m.initRepos,
	})
	m.enterPlanSelect()
}

func (m Model) renderLoading() string {
	var b strings.Builder
	b.WriteString(m.header("init", "Contacting "+m.ownerRef.Host))
	fmt.Fprintf(&b, "  %s Looking up %s and listing its repositories…\n", m.spin.View(), m.ownerRef.Owner)
	b.WriteString(m.footer("esc cancel"))
	return b.String()
}
