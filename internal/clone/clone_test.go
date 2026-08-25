package clone

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IHaveASegway/gitops/internal/format"
	"github.com/IHaveASegway/gitops/internal/github"
	"github.com/IHaveASegway/gitops/internal/runner"
	"github.com/IHaveASegway/gitops/internal/testutil"
)

var acme = github.Owner{Login: "acme", Type: "Organization"}

func acmeRepos(names ...string) []github.Repo {
	var out []github.Repo
	for _, n := range names {
		out = append(out, github.Repo{Name: n, FullName: "acme/" + n, DefaultBranch: "main"})
	}
	return out
}

func actions(p *Plan) map[string]Action {
	m := map[string]Action{}
	for _, e := range p.Entries {
		m[e.Repo.Name] = e.Action
	}
	return m
}

func plan(base string, here bool, repos []github.Repo, f Filter) *Plan {
	return BuildPlan(Request{BaseDir: base, Here: here, Host: "github.com", Owner: acme, Repos: repos, Filter: f})
}

// dupLayout: base/aspyn/{a,b} and base/other/c belong to acme (folder names
// differ from the login); base/loose-d is a loose acme repo directly in
// base; base/x is unrelated.
func dupLayout(t *testing.T) string {
	base := t.TempDir()
	testutil.NewRepo(t, filepath.Join(base, "aspyn", "a"), "https://tok@github.com/Acme/a.git", false)
	testutil.NewRepo(t, filepath.Join(base, "aspyn", "b"), "git@github.com:acme/b.git", false)
	testutil.NewRepo(t, filepath.Join(base, "other", "c"), "https://github.com/acme/c.git", false)
	testutil.NewRepo(t, filepath.Join(base, "loose-d"), "https://github.com/acme/d.git", false)
	testutil.NewRepo(t, filepath.Join(base, "x"), "https://github.com/someone-else/x.git", false)
	return base
}

func TestPlanDetectsExistingCheckouts(t *testing.T) {
	base := dupLayout(t)
	p := plan(base, false, acmeRepos("a", "b", "c", "d", "e"), Filter{})

	if p.TargetDir != filepath.Join(base, "acme") {
		t.Fatalf("target = %s", p.TargetDir)
	}
	for name, a := range actions(p) {
		if a != ActionClone {
			t.Errorf("%s: action %v, want clone (target dir does not exist yet)", name, a)
		}
	}
	if len(p.Warnings) != 3 {
		t.Fatalf("warnings = %+v", p.Warnings)
	}
	if !p.Warnings[0].Nested || p.Warnings[0].Dir != base || strings.Join(p.Warnings[0].Repos, ",") != "loose-d" {
		t.Errorf("warning[0] = %+v", p.Warnings[0])
	}
	if p.Warnings[1].Dir != filepath.Join(base, "aspyn") || strings.Join(p.Warnings[1].Repos, ",") != "a,b" || p.Warnings[1].Nested {
		t.Errorf("warning[1] = %+v", p.Warnings[1])
	}
	if p.Warnings[2].Dir != filepath.Join(base, "other") {
		t.Errorf("warning[2] = %+v", p.Warnings[2])
	}
	lines := strings.Join(p.WarningLines(p.Warnings[1]), "\n")
	if !strings.Contains(lines, "Existing checkout of acme found at") || !strings.Contains(lines, "gitops init acme -d "+format.ShortenPath(filepath.Join(base, "aspyn"))+" --here") {
		t.Errorf("warning text:\n%s", lines)
	}
	nested := strings.Join(p.WarningLines(p.Warnings[0]), "\n")
	if !strings.Contains(nested, "This directory already contains 1 repo from acme") || !strings.Contains(nested, "--here") {
		t.Errorf("nested warning text:\n%s", nested)
	}
}

func TestPlanFromInsideExistingCheckout(t *testing.T) {
	base := dupLayout(t)
	inside := filepath.Join(base, "aspyn")
	p := plan(inside, false, acmeRepos("a", "b", "c"), Filter{})
	if p.TargetDir != filepath.Join(inside, "acme") {
		t.Fatalf("target = %s", p.TargetDir)
	}
	if len(p.Warnings) == 0 || !p.Warnings[0].Nested || strings.Join(p.Warnings[0].Repos, ",") != "a,b" {
		t.Fatalf("expected nested warning first, got %+v", p.Warnings)
	}
	dirs := map[string]bool{}
	for _, w := range p.Warnings {
		dirs[w.Dir] = true
	}
	if !dirs[filepath.Join(base, "other")] || !dirs[base] {
		t.Errorf("parent-level checkouts not detected: %+v", p.Warnings)
	}

	h := plan(inside, true, acmeRepos("a", "b", "c"), Filter{})
	if h.TargetDir != inside {
		t.Fatalf("here target = %s", h.TargetDir)
	}
	if got := actions(h); got["a"] != ActionExists || got["b"] != ActionExists || got["c"] != ActionClone {
		t.Errorf("here actions = %v", got)
	}
	for _, w := range h.Warnings {
		if w.Nested {
			t.Errorf("no nested warning expected with --here: %+v", w)
		}
	}
	if h.SummaryLine() != "3 repos · 1 to clone · 2 already present" {
		t.Errorf("summary = %q", h.SummaryLine())
	}
	if h.SelectionLine(1) != "3 repos · 1 selected to clone · 2 already present" {
		t.Errorf("selection = %q", h.SelectionLine(1))
	}
}

func TestPlanReusesCaseInsensitiveDirAndFlagsConflicts(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "Acme-Org")
	testutil.NewRepo(t, filepath.Join(target, "a"), "https://github.com/acme-org/a.git", false)
	testutil.NewRepo(t, filepath.Join(target, "renamed-b"), "https://github.com/ACME-ORG/b.git", false)
	testutil.NewRepo(t, filepath.Join(target, "c"), "https://github.com/someone-else/c.git", false)
	testutil.NewRepo(t, filepath.Join(target, "d"), "", false)
	testutil.NewRepo(t, filepath.Join(target, ".github"), "https://github.com/acme-org/.github.git", false)
	if err := os.MkdirAll(filepath.Join(target, "e"), 0o755); err != nil {
		t.Fatal(err)
	}
	owner := github.Owner{Login: "acme-org", Type: "Organization"}
	repos := acmeRepos("a", "b", "c", "d", "e", "f", ".github", "../x")
	p := BuildPlan(Request{BaseDir: base, Host: "github.com", Owner: owner, Repos: repos})
	if p.TargetDir != target {
		t.Fatalf("target = %s, want existing %s", p.TargetDir, target)
	}
	got := actions(p)
	want := map[string]Action{"a": ActionExists, "b": ActionExists, "c": ActionConflict, "d": ActionConflict,
		"e": ActionConflict, "f": ActionClone, ".github": ActionExists, "../x": ActionConflict}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %v want %v", k, got[k], v)
		}
	}
	for _, e := range p.Entries {
		switch e.Repo.Name {
		case "b":
			if e.Reason != "as renamed-b" || e.Dest != filepath.Join(target, "renamed-b") {
				t.Errorf("b: %+v", e)
			}
		case "c":
			if !strings.Contains(e.Reason, "different origin (someone-else/c)") {
				t.Errorf("c reason = %q", e.Reason)
			}
		case "d":
			if !strings.Contains(e.Reason, "without an origin") {
				t.Errorf("d reason = %q", e.Reason)
			}
		case "e":
			if !strings.Contains(e.Reason, "not a git repository") {
				t.Errorf("e reason = %q", e.Reason)
			}
		}
	}
	if p.Foreign != 2 || len(p.Warnings) != 0 {
		t.Errorf("foreign=%d warnings=%v", p.Foreign, p.Warnings)
	}
}

func TestPlanFilters(t *testing.T) {
	base := t.TempDir()
	repos := []github.Repo{
		{Name: "app", FullName: "acme/app"},
		{Name: "old", FullName: "acme/old", Archived: true},
		{Name: "fork", FullName: "acme/fork", Fork: true},
	}
	if got := actions(plan(base, false, repos, Filter{})); got["app"] != ActionClone || got["old"] != ActionArchived || got["fork"] != ActionClone {
		t.Errorf("default filter: %v", got)
	}
	if got := actions(plan(base, false, repos, Filter{IncludeArchived: true, SkipForks: true})); got["old"] != ActionClone || got["fork"] != ActionFork {
		t.Errorf("archived/no-forks: %v", got)
	}
	p := plan(base, false, repos, Filter{Only: map[string]bool{"app": true, "missing": true}})
	if got := actions(p); got["app"] != ActionClone || got["old"] != ActionFiltered || got["fork"] != ActionFiltered {
		t.Errorf("only: %v", got)
	}
	if strings.Join(p.Missing, ",") != "missing" || len(p.Considered()) != 1 {
		t.Errorf("missing=%v considered=%d", p.Missing, len(p.Considered()))
	}
	if ActionClone.String() != "clone" || ActionConflict.String() != "conflict" || Action(99).String() != "?" {
		t.Error("Action.String mismatch")
	}
}

func TestEnvAndCleanError(t *testing.T) {
	if got := env(Options{Host: "github.com", Protocol: "ssh", Token: "t"}); got != nil {
		t.Errorf("ssh should not inject a token: %v", got)
	}
	if got := env(Options{Host: "github.com", Protocol: "https"}); got != nil {
		t.Errorf("no token: %v", got)
	}
	joined := strings.Join(env(Options{Host: "github.com", Protocol: "https", Token: "secret"}), "\n")
	if !strings.Contains(joined, "GIT_CONFIG_COUNT=1") ||
		!strings.Contains(joined, "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader") ||
		!strings.Contains(joined, "AUTHORIZATION: basic eC1hY2Nlc3MtdG9rZW46c2VjcmV0") ||
		strings.Contains(joined, "secret") {
		t.Errorf("env = %q", joined)
	}

	msg := "Cloning into '/tmp/x'...\nremote: Repository not found.\nfatal: repository 'https://github.com/a/b.git/' not found"
	if got := cleanError(msg); got != "fatal: repository 'https://github.com/a/b.git/' not found" {
		t.Errorf("got %q", got)
	}
	if got := cleanError("Cloning into 'x'...\nsomething odd"); got != "something odd" {
		t.Errorf("got %q", got)
	}
}

func TestRunEndToEnd(t *testing.T) {
	root := t.TempDir()
	urlA := testutil.NewBare(t, root, "a")
	urlB := testutil.NewBare(t, root, "b")
	base := filepath.Join(root, "base")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	repos := []github.Repo{
		{Name: "a", FullName: "acme/a", DefaultBranch: "main", CloneURL: urlA},
		{Name: "b", FullName: "acme/b", DefaultBranch: "main", CloneURL: urlB},
		{Name: "broken", FullName: "acme/broken", DefaultBranch: "main", CloneURL: testutil.FileURL(filepath.Join(root, "missing.git"))},
	}
	p := plan(base, false, repos, Filter{})
	if n := len(p.ToClone()); n != 3 {
		t.Fatalf("to clone = %d", n)
	}
	var started, finished int
	results := Run(context.Background(), p.ToClone(), Options{Host: "github.com", Protocol: "https", Token: "tok"}, 2, func(ev runner.Event) {
		if ev.Started {
			started++
		} else {
			finished++
		}
	})
	if started != 3 || finished != 3 {
		t.Errorf("events started=%d finished=%d", started, finished)
	}
	byName := map[string]runner.Result{}
	for _, r := range results {
		byName[r.Repo] = r
	}
	if !byName["a"].Success || !byName["b"].Success || byName["a"].Output != "cloned (main)" {
		t.Errorf("results = %+v", results)
	}
	if byName["broken"].Success || !strings.HasPrefix(byName["broken"].Error, "clone: ") {
		t.Errorf("broken = %+v", byName["broken"])
	}
	if pathExists(filepath.Join(base, "acme", "broken")) {
		t.Error("failed clone left a directory behind")
	}

	// Make the clones look like real GitHub checkouts and re-plan.
	testutil.Git(t, filepath.Join(base, "acme", "a"), "remote", "set-url", "origin", "https://github.com/acme/a.git")
	testutil.Git(t, filepath.Join(base, "acme", "b"), "remote", "set-url", "origin", "git@github.com:acme/b.git")
	again := plan(base, false, repos, Filter{})
	if got := actions(again); got["a"] != ActionExists || got["b"] != ActionExists || got["broken"] != ActionClone {
		t.Errorf("re-plan = %v", got)
	}
	if len(again.Warnings) != 0 {
		t.Errorf("unexpected warnings: %+v", again.Warnings)
	}

	var b strings.Builder
	again.Print(&b, "https", true)
	out := b.String()
	for _, want := range []string{"Organization: acme", "https://github.com/acme", "3 repos · 1 to clone · 2 already present", "= a", "+ broken"} {
		if !strings.Contains(out, want) {
			t.Errorf("Print output is missing %q:\n%s", want, out)
		}
	}
}
