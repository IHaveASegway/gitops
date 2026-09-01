// Package tui implements the interactive terminal UI: a menu of operations,
// a repository picker, confirmation for destructive actions, live progress
// while operations run, and a browsable results view.
package tui

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/IHaveASegway/gitops/internal/clone"
	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/github"
	"github.com/IHaveASegway/gitops/internal/report"
	"github.com/IHaveASegway/gitops/internal/runner"
)

type view int

const (
	viewMenu view = iota
	viewPicker
	viewInput
	viewLoading
	viewConfirm
	viewExec
	viewResults
)

// opDef describes a menu entry.
type opDef struct {
	name        string
	desc        string
	input       string // label of the text prompt; "" when the op needs no input
	placeholder string
	destructive bool
	needsRepos  bool
}

var opDefs = []opDef{
	{name: "pull", desc: "Checkout default branch and pull latest", needsRepos: true},
	{name: "sync", desc: "Stash changes, pull default branch, restore stash", needsRepos: true},
	{name: "status", desc: "Branch, ahead/behind and working tree status", needsRepos: true},
	{name: "branch", desc: "Create a new branch from the default branch", input: "Branch name", placeholder: "feature/my-branch", needsRepos: true},
	{name: "checkout", desc: "Checkout an existing branch", input: "Branch name", placeholder: "feature/existing-branch", needsRepos: true},
	{name: "push", desc: "Stage all, commit and push current branch", input: "Commit message", placeholder: "fix: update something", destructive: true, needsRepos: true},
	{name: "reset", desc: "Discard ALL local changes, force checkout default, pull", destructive: true, needsRepos: true},
	{name: "init", desc: "Clone every repo of a GitHub org into a subdirectory", input: "GitHub organization URL or name", placeholder: "https://github.com/my-org"},
}

type repoItem struct {
	path   string
	name   string
	info   git.Info
	loaded bool
}

// listItem is a selectable row in the picker: a local repo (repoIdx) or an
// init plan entry (planIdx).
type listItem struct {
	name       string
	repoIdx    int
	planIdx    int
	selectable bool
}

type execState int

const (
	stPending execState = iota
	stRunning
	stDone
)

type runRecord struct {
	title   string
	results []runner.Result
}

// Messages delivered to Update.
type (
	repoInfoMsg struct {
		ch       chan repoInfoMsg
		gen, idx int
		info     git.Info
	}
	infoDoneMsg   struct{ gen int }
	initLoadedMsg struct {
		gen        int
		owner      github.Owner
		repos      []github.Repo
		opts       clone.Options
		tokenSrc   string
		apiBase    string
		overridden bool
		err        error
	}
	execEventMsg    runner.Event
	execFinishedMsg struct{}
)

// Model is the Bubble Tea model for the whole application.
type Model struct {
	baseDir   string
	jobs      int
	preselect map[string]bool

	view   view
	width  int
	height int
	flash  string
	quit   bool

	opIdx int

	repos       []repoItem
	infoGen     int
	infoCh      chan repoInfoMsg
	infoLoading bool

	// picker
	items     []listItem
	selected  []bool
	visible   []int
	cursor    int
	scroll    int
	filter    textinput.Model
	filtering bool

	// text input
	input    textinput.Model
	inputErr string

	// init flow
	ownerRef    github.OwnerRef
	initGen     int
	initOwner   github.Owner
	initRepos   []github.Repo
	initOpts    clone.Options
	tokenSrc    string
	apiBase     string
	apiOverride bool
	plan        *clone.Plan

	// execution
	execTitle  string
	targets    []string
	states     []execState
	results    []runner.Result
	done       int
	events     chan runner.Event
	cancel     context.CancelFunc
	cancelling bool
	quitAfter  bool
	spin       spinner.Model
	prog       progress.Model

	// results
	doneCursor int
	doneScroll int
	expanded   map[int]bool
	failOnly   bool
	history    []runRecord
}

// New creates a model operating on baseDir. preselect names repos that
// start out checked in the first picker (all repos when empty).
func New(baseDir string, jobs int, preselect []string) Model {
	ti := textinput.New()
	ti.CharLimit = 512
	ti.Width = 50
	ti.Prompt = "> "

	fi := textinput.New()
	fi.CharLimit = 64
	fi.Width = 30
	fi.Prompt = "/ "
	fi.Placeholder = "type to filter"

	m := Model{
		baseDir:  baseDir,
		jobs:     jobs,
		input:    ti,
		filter:   fi,
		spin:     spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(spinnerStyle)),
		prog:     progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage()),
		expanded: map[int]bool{},
	}
	if len(preselect) > 0 {
		m.preselect = map[string]bool{}
		for _, n := range preselect {
			m.preselect[strings.ToLower(strings.TrimSpace(n))] = true
		}
	}
	m.loadRepos()
	return m
}

// Run starts the TUI and, once it exits, prints every completed run to
// stdout so the results survive leaving the alternate screen.
func Run(baseDir string, jobs int, preselect []string) error {
	p := tea.NewProgram(New(baseDir, jobs, preselect), tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		return err
	}
	if m, ok := final.(Model); ok {
		m.PrintHistory(os.Stdout)
	}
	return nil
}

// PrintHistory writes every completed run to w.
func (m Model) PrintHistory(w io.Writer) {
	for _, h := range m.history {
		report.PrintResults(w, h.title+" results", h.results)
	}
}

// loadRepos (re)discovers repos in baseDir and starts loading their status
// in the background with bounded parallelism.
func (m *Model) loadRepos() {
	paths, _ := git.Discover(m.baseDir)
	m.repos = make([]repoItem, len(paths))
	for i, p := range paths {
		m.repos[i] = repoItem{path: p, name: filepath.Base(p)}
	}
	m.infoGen++
	gen := m.infoGen
	ch := make(chan repoInfoMsg, 16)
	m.infoCh = ch
	m.infoLoading = len(paths) > 0
	jobs := max(1, m.jobs)
	go func() {
		sem := make(chan struct{}, jobs)
		var wg sync.WaitGroup
		for i, p := range paths {
			wg.Add(1)
			go func(i int, p string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				ch <- repoInfoMsg{ch: ch, gen: gen, idx: i, info: git.Inspect(context.Background(), p)}
			}(i, p)
		}
		wg.Wait()
		close(ch)
	}()
}

func waitInfo(ch chan repoInfoMsg, gen int) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return infoDoneMsg{gen: gen}
		}
		return msg
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return waitInfo(m.infoCh, m.infoGen)
}

func (m Model) op() opDef    { return opDefs[m.opIdx] }
func (m Model) isInit() bool { return m.op().name == "init" }

// listHeight is the number of rows available to lists.
func (m Model) listHeight() int {
	h := m.height - 9
	if m.filtering {
		h--
	}
	return max(3, h)
}

// repoSummary counts dirty/behind/ahead repos once their status has loaded.
func (m Model) repoSummary() (dirty, behind, ahead int) {
	for _, r := range m.repos {
		if !r.loaded {
			continue
		}
		if r.info.Dirty > 0 {
			dirty++
		}
		if r.info.Behind > 0 {
			behind++
		}
		if r.info.Ahead > 0 {
			ahead++
		}
	}
	return dirty, behind, ahead
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(20, min(100, m.width-8))
		m.prog.Width = max(10, min(40, m.width-30))
		return m, nil

	case repoInfoMsg:
		if msg.gen != m.infoGen {
			return m, waitInfo(msg.ch, msg.gen) // stale loader: keep draining it
		}
		if msg.idx < len(m.repos) {
			m.repos[msg.idx].info = msg.info
			m.repos[msg.idx].loaded = true
		}
		return m, waitInfo(m.infoCh, m.infoGen)

	case infoDoneMsg:
		if msg.gen == m.infoGen {
			m.infoLoading = false
		}
		return m, nil

	case initLoadedMsg:
		return m.handleInitLoaded(msg)

	case execEventMsg, execFinishedMsg:
		return m.updateExec(msg)

	case spinner.TickMsg:
		if m.view == viewExec || m.view == viewLoading {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			return m.handleKey("up")
		case tea.MouseButtonWheelDown:
			return m.handleKey("down")
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m.handleCtrlC()
		}
		m.flash = ""
		if m.view == viewInput || (m.view == viewPicker && m.filtering) {
			return m.handleTextKey(msg)
		}
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m Model) handleCtrlC() (tea.Model, tea.Cmd) {
	if m.view == viewExec {
		if m.cancelling {
			m.quit = true
			return m, tea.Quit
		}
		m.cancelling, m.quitAfter = true, true
		m.cancel()
		m.flash = "Cancelling — waiting for running git processes to stop…"
		return m, nil
	}
	m.quit = true
	return m, tea.Quit
}

// handleTextKey routes keys to the focused text field (input or filter).
func (m Model) handleTextKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc", "up", "down", "pgup", "pgdown":
		return m.handleKey(msg.String())
	}
	var cmd tea.Cmd
	if m.view == viewInput {
		m.inputErr = ""
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	m.filter, cmd = m.filter.Update(msg)
	m.applyFilter()
	return m, cmd
}

func (m Model) handleKey(key string) (tea.Model, tea.Cmd) {
	switch m.view {
	case viewMenu:
		return m.updateMenu(key)
	case viewPicker:
		return m.updatePicker(key)
	case viewInput:
		return m.updateInput(key)
	case viewLoading:
		if key == "esc" {
			m.initGen++ // ignore the pending result
			m.view = viewInput
		}
		return m, nil
	case viewConfirm:
		return m.updateConfirm(key)
	case viewExec:
		if key == "esc" && !m.cancelling {
			m.cancelling = true
			m.cancel()
			m.flash = "Cancelling — waiting for running git processes to stop…"
		}
		return m, nil
	case viewResults:
		return m.updateResults(key)
	}
	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if m.quit {
		return ""
	}
	switch m.view {
	case viewMenu:
		return m.renderMenu()
	case viewPicker:
		return m.renderPicker()
	case viewInput:
		return m.renderInput()
	case viewLoading:
		return m.renderLoading()
	case viewConfirm:
		return m.renderConfirm()
	case viewExec:
		return m.renderExec()
	case viewResults:
		return m.renderResults()
	}
	return ""
}

func (m Model) flashf(format string, args ...any) Model {
	m.flash = fmt.Sprintf(format, args...)
	return m
}
