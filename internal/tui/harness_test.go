package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/IHaveASegway/gitops/internal/testutil"
)

// pump executes cmd and feeds the resulting messages back into the model
// until no command is left. Spinner ticks are applied once but not
// re-armed, so the tick chain cannot loop forever.
func pump(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	var run func(cmd tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		switch msg := msg.(type) {
		case nil, tea.QuitMsg:
			return
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
			return
		case spinner.TickMsg:
			mm, _ := m.Update(msg)
			m = mm.(Model)
			return
		}
		mm, next := m.Update(msg)
		m = mm.(Model)
		run(next)
	}
	run(cmd)
	return m
}

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		mm, cmd := m.Update(keyMsg(k))
		m = pump(t, mm.(Model), cmd)
	}
	return m
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = press(t, m, string(r))
	}
	return m
}

// fixture creates three clones of a local upstream (beta dirty) and a model
// that has finished loading their status.
func fixture(t *testing.T) (string, Model) {
	t.Helper()
	root := t.TempDir()
	up := testutil.NewBare(t, root, "up")
	base := filepath.Join(root, "base")
	for _, n := range []string{"alpha", "beta", "gamma"} {
		testutil.Git(t, root, "clone", "-q", up, filepath.Join(base, n))
	}
	if err := os.WriteFile(filepath.Join(base, "beta", "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(base, 2, nil)
	// Static cursors: otherwise every simulated keypress waits for a blink tick.
	m.input.Cursor.SetMode(cursor.CursorStatic)
	m.filter.Cursor.SetMode(cursor.CursorStatic)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = pump(t, mm.(Model), m.Init())
	return base, m
}

func selectOp(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = press(t, m, "home")
	for m.op().name != name {
		m = press(t, m, "down")
	}
	return m
}

func assertView(t *testing.T, m Model, want ...string) {
	t.Helper()
	v := m.View()
	for _, w := range want {
		if !strings.Contains(v, w) {
			t.Errorf("view is missing %q:\n%s", w, v)
		}
	}
}
