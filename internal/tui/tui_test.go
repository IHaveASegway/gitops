package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/github"
	"github.com/IHaveASegway/gitops/internal/github/githubtest"
	"github.com/IHaveASegway/gitops/internal/testutil"
)

func TestStatusFlow(t *testing.T) {
	_, m := fixture(t)
	if len(m.repos) != 3 || m.infoLoading {
		t.Fatalf("repos=%d loading=%v", len(m.repos), m.infoLoading)
	}
	for _, r := range m.repos {
		if !r.loaded {
			t.Fatalf("%s status not loaded", r.name)
		}
	}
	if m.repos[1].info.Dirty != 1 {
		t.Errorf("beta info = %+v", m.repos[1].info)
	}
	assertView(t, m, "3 repos", "1 dirty", "▸ pull")

	m = selectOp(t, m, "status")
	m = press(t, m, "enter")
	if m.view != viewPicker || len(m.items) != 3 || m.selectedCount() != 3 {
		t.Fatalf("view=%v items=%d selected=%d", m.view, len(m.items), m.selectedCount())
	}
	assertView(t, m, "[x] alpha", "[x] beta", "*1", "3 of 3 selected")

	m = press(t, m, "n", "enter")
	if m.view != viewPicker || !strings.Contains(m.flash, "Select at least one") {
		t.Fatalf("empty selection should be refused: view=%v flash=%q", m.view, m.flash)
	}

	m = press(t, m, "/")
	m = typeText(t, m, "bet")
	if !m.filtering || len(m.visible) != 1 || m.items[m.visible[0]].name != "beta" {
		t.Fatalf("filter: filtering=%v visible=%v", m.filtering, m.visible)
	}
	m = press(t, m, "enter", "a") // apply filter, select all visible (= beta)
	if m.selectedCount() != 1 {
		t.Fatalf("selected=%d", m.selectedCount())
	}
	m = press(t, m, "esc") // clears the filter, stays in the picker
	if m.view != viewPicker || len(m.visible) != 3 || m.selectedCount() != 1 {
		t.Fatalf("after esc: view=%v visible=%d selected=%d", m.view, len(m.visible), m.selectedCount())
	}

	m = press(t, m, "enter")
	if m.view != viewResults || len(m.results) != 1 || len(m.history) != 1 {
		t.Fatalf("view=%v results=%d history=%d", m.view, len(m.results), len(m.history))
	}
	r := m.results[0]
	if r.Repo != "beta" || !r.Success || !strings.HasPrefix(r.Output, "[main] 1 changed") {
		t.Fatalf("result = %+v", r)
	}
	assertView(t, m, "status · done", "✓ 1", "✗ 0", "beta")
	m = press(t, m, "enter")
	assertView(t, m, "?? dirty.txt")

	var out strings.Builder
	m.PrintHistory(&out)
	if !strings.Contains(out.String(), "status results") || !strings.Contains(out.String(), "?? dirty.txt") {
		t.Errorf("PrintHistory output:\n%s", out.String())
	}

	m = press(t, m, "esc")
	if m.view != viewMenu || m.infoLoading {
		t.Fatalf("back to menu: view=%v loading=%v", m.view, m.infoLoading)
	}
	m = press(t, m, "q")
	if !m.quit || m.View() != "" {
		t.Fatal("q should quit")
	}
}

func TestDestructiveConfirm(t *testing.T) {
	_, m := fixture(t)
	m = selectOp(t, m, "reset")
	m = press(t, m, "enter", "enter")
	if m.view != viewConfirm {
		t.Fatalf("view=%v", m.view)
	}
	assertView(t, m, "permanently discard", "3 repositories", "alpha, beta, gamma", "y confirm")
	m = press(t, m, "esc")
	if m.view != viewPicker {
		t.Fatalf("esc should return to the picker, got %v", m.view)
	}
	m = press(t, m, "enter", "y")
	if m.view != viewResults || len(m.results) != 3 {
		t.Fatalf("view=%v results=%d", m.view, len(m.results))
	}
	for _, r := range m.results {
		if !r.Success || r.Output != "reset to main" {
			t.Errorf("result = %+v", r)
		}
	}
	m = press(t, m, "f")
	assertView(t, m, "showing failures only", "(no failures)")
	m = press(t, m, "f", "e")
	assertView(t, m, "(no further output)")
}

func TestInputValidation(t *testing.T) {
	_, m := fixture(t)
	m = selectOp(t, m, "branch")
	m = press(t, m, "enter", "enter")
	if m.view != viewInput {
		t.Fatalf("view=%v", m.view)
	}
	m = press(t, m, "enter")
	if m.inputErr != "Branch name is required" {
		t.Errorf("inputErr = %q", m.inputErr)
	}
	m = typeText(t, m, "bad name")
	m = press(t, m, "enter")
	if !strings.Contains(m.inputErr, "not a valid branch name") || m.view != viewInput {
		t.Errorf("inputErr=%q view=%v", m.inputErr, m.view)
	}
	m = press(t, m, "esc")
	if m.view != viewPicker {
		t.Errorf("esc from input should return to picker, got %v", m.view)
	}

	m = press(t, m, "esc")
	m = selectOp(t, m, "init")
	m = press(t, m, "enter")
	m = typeText(t, m, "acme!")
	m = press(t, m, "enter")
	if m.view != viewInput || !strings.Contains(m.inputErr, "not a valid GitHub login") {
		t.Errorf("view=%v inputErr=%q", m.view, m.inputErr)
	}
	m = press(t, m, "esc")
	if m.view != viewMenu {
		t.Errorf("esc from init input should return to menu, got %v", m.view)
	}
}

func TestInitFlow(t *testing.T) {
	root := t.TempDir()
	urlA := testutil.NewBare(t, root, "a")
	urlB := testutil.NewBare(t, root, "b")
	repos := []github.Repo{
		{Name: "a", FullName: "acme/a", DefaultBranch: "main", CloneURL: urlA, SSHURL: urlA, Private: true},
		{Name: "b", FullName: "acme/b", DefaultBranch: "main", CloneURL: urlB, SSHURL: urlB},
	}
	srv := githubtest.NewServer(t, "acme", repos)
	t.Setenv("GITOPS_GITHUB_API", srv.URL)
	t.Setenv("GH_TOKEN", githubtest.Token)

	base, m := fixture(t)
	m = selectOp(t, m, "init")
	m = press(t, m, "enter")
	assertView(t, m, "GitHub organization URL or name", "Accepts https://github.com/<org>")
	m = typeText(t, m, "https://github.com/acme")
	m = press(t, m, "enter")
	if m.view != viewPicker || m.plan == nil || len(m.items) != 2 || m.selectedCount() != 2 {
		t.Fatalf("after load: view=%v plan=%v items=%d inputErr=%q", m.view, m.plan != nil, len(m.items), m.inputErr)
	}
	if m.plan.TargetDir != filepath.Join(base, "Acme") {
		t.Errorf("target = %s", m.plan.TargetDir)
	}
	assertView(t, m, "init › Acme", "2 selected to clone", "clone · private")

	m = press(t, m, "enter")
	if m.view != viewConfirm {
		t.Fatalf("view=%v", m.view)
	}
	assertView(t, m, "Review before cloning", "Organization", "Target", "token from $GH_TOKEN", "y clone")

	m = press(t, m, "y")
	if m.view != viewResults || len(m.results) != 2 {
		t.Fatalf("view=%v results=%+v", m.view, m.results)
	}
	for _, r := range m.results {
		if !r.Success || r.Output != "cloned (main)" {
			t.Errorf("result = %+v", r)
		}
	}
	if !git.IsRepo(filepath.Join(base, "Acme", "a")) || !git.IsRepo(filepath.Join(base, "Acme", "b")) {
		t.Fatal("clones missing")
	}
	assertView(t, m, "init Acme · done", "esc open org dir")

	m = press(t, m, "esc")
	if m.view != viewMenu || m.baseDir != filepath.Join(base, "Acme") || len(m.repos) != 2 {
		t.Fatalf("after init: view=%v base=%s repos=%d", m.view, m.baseDir, len(m.repos))
	}
	assertView(t, m, "Now operating on")
}

func TestViewsFitNarrowTerminal(t *testing.T) {
	_, m := fixture(t)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 44, Height: 14})
	m = mm.(Model)
	check := func(label string) {
		t.Helper()
		for i, line := range strings.Split(m.View(), "\n") {
			if w := lipgloss.Width(line); w > 44 {
				t.Errorf("%s line %d is %d cells wide: %q", label, i, w, line)
			}
		}
	}
	check("menu")
	m = selectOp(t, m, "status")
	m = press(t, m, "enter")
	check("picker")
	m = press(t, m, "enter")
	check("results")
	m = press(t, m, "e")
	check("results expanded")
}
