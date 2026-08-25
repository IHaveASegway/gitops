// Package clone plans and performs cloning a whole GitHub organization,
// detecting checkouts that already exist so an org is never duplicated by
// accident.
package clone

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/IHaveASegway/gitops/internal/format"
	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/github"
)

// Action is what the plan will do with a repository.
type Action int

const (
	ActionClone    Action = iota // will be cloned
	ActionExists                 // already cloned inside the target directory
	ActionArchived               // skipped: archived on GitHub
	ActionFork                   // skipped: fork (Filter.SkipForks)
	ActionConflict               // destination path exists but is not this repo
	ActionFiltered               // excluded by Filter.Only
)

func (a Action) String() string {
	switch a {
	case ActionClone:
		return "clone"
	case ActionExists:
		return "exists"
	case ActionArchived:
		return "archived"
	case ActionFork:
		return "fork"
	case ActionConflict:
		return "conflict"
	case ActionFiltered:
		return "filtered"
	}
	return "?"
}

// Entry is one repository in a plan.
type Entry struct {
	Repo   github.Repo
	Action Action
	Dest   string // where the repo lives, or would be cloned
	Reason string // detail for exists/conflict entries
}

// Warning describes repositories of the org that already exist outside the
// target directory — cloning again would duplicate them.
type Warning struct {
	Dir    string   // directory that holds the repos
	Repos  []string // directory names of those repos, sorted
	Nested bool     // Dir is the base dir itself: the new checkout would nest inside the old one
}

// Filter narrows which repositories are cloned.
type Filter struct {
	Only            map[string]bool // lowercase repo names; nil means all
	IncludeArchived bool
	SkipForks       bool
}

// Request describes what to plan.
type Request struct {
	BaseDir string        // directory the org folder is created in
	Here    bool          // clone into BaseDir itself instead of BaseDir/<org>
	Host    string        // git host, e.g. "github.com"
	Owner   github.Owner  // resolved organization or user
	Repos   []github.Repo // repositories reported by the API
	Filter  Filter
}

// Plan is the fully resolved outcome of a Request before any cloning.
type Plan struct {
	Host      string
	Owner     github.Owner
	BaseDir   string
	TargetDir string
	Here      bool
	Entries   []Entry
	Warnings  []Warning
	Missing   []string // Filter.Only names that do not exist in the org
	Foreign   int      // repos inside TargetDir that belong to other owners
}

// Count returns how many entries have the given action.
func (p *Plan) Count(a Action) int {
	n := 0
	for _, e := range p.Entries {
		if e.Action == a {
			n++
		}
	}
	return n
}

// Considered returns the entries not excluded by Filter.Only.
func (p *Plan) Considered() []Entry {
	var out []Entry
	for _, e := range p.Entries {
		if e.Action != ActionFiltered {
			out = append(out, e)
		}
	}
	return out
}

// ToClone returns the entries that will be cloned.
func (p *Plan) ToClone() []Entry {
	var out []Entry
	for _, e := range p.Entries {
		if e.Action == ActionClone {
			out = append(out, e)
		}
	}
	return out
}

// HasWarnings reports whether existing checkouts were detected.
func (p *Plan) HasWarnings() bool { return len(p.Warnings) > 0 }

// OwnerURL is the browsable URL of the org/user.
func (p *Plan) OwnerURL() string {
	return github.OwnerRef{Host: p.Host, Owner: p.Owner.Login}.URL()
}

// resolveTargetDir picks the directory the org is cloned into: the base dir
// itself with Here, otherwise an existing child that matches the login
// case-insensitively (so "Acme" and "acme" never end up side by side),
// otherwise a new child named after the canonical login.
func resolveTargetDir(baseDir, login string, here bool) string {
	if here {
		return baseDir
	}
	if entries, err := os.ReadDir(baseDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.EqualFold(e.Name(), login) {
				return filepath.Join(baseDir, e.Name())
			}
		}
	}
	return filepath.Join(baseDir, login)
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// validRepoDirName rejects names that could escape the target directory.
func validRepoDirName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`)
}

// BuildPlan decides what to do with each repository and looks for existing
// checkouts of the same org that a fresh clone would duplicate.
func BuildPlan(req Request) *Plan {
	baseDir := filepath.Clean(req.BaseDir)
	p := &Plan{Host: req.Host, Owner: req.Owner, BaseDir: baseDir, Here: req.Here}
	p.TargetDir = resolveTargetDir(baseDir, req.Owner.Login, req.Here)

	// Look around: the base dir and its parent, two levels deep each. This
	// catches loose repos next to the target, org folders named differently
	// from the login, and running init from inside an existing checkout.
	seen := map[string]bool{}
	var local []git.LocalRepo
	collect := func(root string, includeHidden bool) {
		for _, r := range git.Scan(root, 2, includeHidden) {
			if !seen[r.Path] {
				seen[r.Path] = true
				local = append(local, r)
			}
		}
	}
	collect(baseDir, true)
	if parent := filepath.Dir(baseDir); parent != baseDir {
		collect(parent, false) // dotfile managers and tool caches live here; skip them
	}

	existing := map[string]git.LocalRepo{} // lowercase remote repo name -> checkout inside target
	dups := map[string][]string{}
	for _, lr := range local {
		inTarget := filepath.Dir(lr.Path) == p.TargetDir
		matches := lr.HasRemote && lr.Remote.IsOwner(req.Host, req.Owner.Login)
		switch {
		case inTarget && matches:
			existing[strings.ToLower(lr.Remote.Repo)] = lr
		case inTarget:
			p.Foreign++
		case matches:
			dir := filepath.Dir(lr.Path)
			dups[dir] = append(dups[dir], filepath.Base(lr.Path))
		}
	}
	for dir, names := range dups {
		sort.Strings(names)
		p.Warnings = append(p.Warnings, Warning{Dir: dir, Repos: names, Nested: dir == baseDir})
	}
	sort.Slice(p.Warnings, func(i, j int) bool {
		a, b := p.Warnings[i], p.Warnings[j]
		if a.Nested != b.Nested {
			return a.Nested
		}
		if len(a.Repos) != len(b.Repos) {
			return len(a.Repos) > len(b.Repos)
		}
		return a.Dir < b.Dir
	})

	matched := map[string]bool{}
	for _, r := range req.Repos {
		lname := strings.ToLower(r.Name)
		e := Entry{Repo: r, Dest: filepath.Join(p.TargetDir, r.Name)}
		if req.Filter.Only != nil && !req.Filter.Only[lname] {
			e.Action = ActionFiltered
			p.Entries = append(p.Entries, e)
			continue
		}
		matched[lname] = true
		switch lr, ok := existing[lname]; {
		case ok:
			e.Action, e.Dest = ActionExists, lr.Path
			if !strings.EqualFold(filepath.Base(lr.Path), r.Name) {
				e.Reason = "as " + filepath.Base(lr.Path)
			}
		case r.Archived && !req.Filter.IncludeArchived:
			e.Action = ActionArchived
		case r.Fork && req.Filter.SkipForks:
			e.Action = ActionFork
		case !validRepoDirName(r.Name):
			e.Action, e.Reason = ActionConflict, "unsafe repository name"
		case pathExists(e.Dest):
			e.Action, e.Reason = classifyExisting(e.Dest, req.Host, req.Owner.Login, r.Name)
		default:
			e.Action = ActionClone
		}
		p.Entries = append(p.Entries, e)
	}
	for name := range req.Filter.Only {
		if !matched[name] {
			p.Missing = append(p.Missing, name)
		}
	}
	sort.Strings(p.Missing)
	return p
}

// classifyExisting decides whether an existing destination path already is
// the wanted repository or conflicts with it.
func classifyExisting(dest, host, owner, repo string) (Action, string) {
	if !git.IsRepo(dest) {
		return ActionConflict, "path exists and is not a git repository"
	}
	u, ok := git.OriginURL(dest)
	if !ok {
		return ActionConflict, "exists as a git repo without an origin remote"
	}
	ref, ok := git.ParseRemoteURL(u)
	if !ok {
		return ActionConflict, "exists with an unrecognized origin"
	}
	if ref.IsRepo(host, owner, repo) {
		return ActionExists, ""
	}
	return ActionConflict, "exists with a different origin (" + ref.String() + ")"
}

// WarningLines renders a warning as plain text lines (no color), including
// the exact command that avoids the duplicate.
func (p *Plan) WarningLines(w Warning) []string {
	login := p.Owner.Login
	names := format.JoinNames(w.Repos, 5)
	if w.Nested {
		return []string{
			fmt.Sprintf("This directory already contains %s from %s (%s).", format.Plural(len(w.Repos), "repo"), login, names),
			fmt.Sprintf("Cloning into %s would create a second, nested copy of the org.", format.ShortenPath(p.TargetDir)),
			fmt.Sprintf("To add only the missing repos here instead:  gitops init %s -d %s --here", login, format.ShortenPath(p.BaseDir)),
		}
	}
	return []string{
		fmt.Sprintf("Existing checkout of %s found at %s (%s: %s).", login, format.ShortenPath(w.Dir), format.Plural(len(w.Repos), "repo"), names),
		fmt.Sprintf("Cloning into %s would duplicate those repos.", format.ShortenPath(p.TargetDir)),
		fmt.Sprintf("To add only the missing repos to that checkout instead:  gitops init %s -d %s --here", login, format.ShortenPath(w.Dir)),
	}
}

// SkippedParts lists the non-clone buckets: "19 archived skipped", …
func (p *Plan) SkippedParts() []string {
	var parts []string
	if n := p.Count(ActionExists); n > 0 {
		parts = append(parts, fmt.Sprintf("%d already present", n))
	}
	if n := p.Count(ActionArchived); n > 0 {
		parts = append(parts, fmt.Sprintf("%d archived skipped", n))
	}
	if n := p.Count(ActionFork); n > 0 {
		parts = append(parts, fmt.Sprintf("%d forks skipped", n))
	}
	if n := p.Count(ActionConflict); n > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", n))
	}
	return parts
}

// SummaryLine renders "98 repos · 79 to clone · 19 archived skipped …".
func (p *Plan) SummaryLine() string {
	parts := append([]string{format.Plural(len(p.Considered()), "repo"), fmt.Sprintf("%d to clone", p.Count(ActionClone))}, p.SkippedParts()...)
	return strings.Join(parts, " · ")
}

// SelectionLine renders "98 repos · 60 selected to clone · 19 archived skipped …".
func (p *Plan) SelectionLine(selected int) string {
	parts := append([]string{format.Plural(len(p.Considered()), "repo"), fmt.Sprintf("%d selected to clone", selected)}, p.SkippedParts()...)
	return strings.Join(parts, " · ")
}
