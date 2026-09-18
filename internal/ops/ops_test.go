package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IHaveASegway/gitops/internal/runner"
	"github.com/IHaveASegway/gitops/internal/testutil"
)

// clone creates a clone of a fresh upstream and returns its path.
func clone(t *testing.T) string {
	t.Helper()
	testutil.Identity(t)
	root := t.TempDir()
	up := testutil.NewBare(t, root, "up")
	p := filepath.Join(root, "clone")
	testutil.Git(t, root, "clone", "-q", up, p)
	// Deterministic line endings on Windows runners (core.autocrlf=true
	// globally there would rewrite "\n" to "\r\n" on checkout).
	testutil.Git(t, p, "config", "core.autocrlf", "false")
	return p
}

func TestStatusSyncResetPull(t *testing.T) {
	ctx := context.Background()
	repo := clone(t)

	if r := Status(ctx, repo); !r.Success || r.Output != "[main] clean" {
		t.Errorf("clean status = %+v", r)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := Status(ctx, repo)
	if !r.Success || !strings.HasPrefix(r.Output, "[main] 1 changed\n") || !strings.Contains(r.Output, "M README.md") {
		t.Errorf("dirty status = %+v", r)
	}
	if r := Sync(false)(ctx, repo); !r.Success || r.Output != "already up to date + stash restored" {
		t.Errorf("sync = %+v", r)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(data) != "changed" {
		t.Error("sync did not restore the local change")
	}
	if r := Reset(false)(ctx, repo); !r.Success || r.Output != "reset to main" {
		t.Errorf("reset = %+v", r)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(data) != "hi\n" {
		t.Error("reset did not discard the local change")
	}
	if r := Pull(false)(ctx, repo); !r.Success || r.Output != "Already up to date." {
		t.Errorf("pull = %+v", r)
	}
}

// superproject is a repository declaring two submodules, vendor/a and
// vendor/b, published to a bare upstream.
type superproject struct {
	root  string // temp dir holding every repository
	work  string // where the superproject is authored; publishes to bare
	bare  string // the superproject's upstream
	clone string // a plain (non-recursive) clone: submodules registered, none initialized
}

func newSuperproject(t *testing.T) superproject {
	t.Helper()
	testutil.Identity(t)
	t.Setenv("GIT_ALLOW_PROTOCOL", "file") // the submodules' origins are local file:// paths
	root := t.TempDir()
	s := superproject{root: root, work: filepath.Join(root, "work"), bare: filepath.Join(root, "parent.git"), clone: filepath.Join(root, "clone")}

	testutil.NewRepo(t, s.work, "", true)
	for _, name := range []string{"a", "b"} {
		lib := testutil.NewBare(t, root, name)
		testutil.Git(t, s.work, "-c", "protocol.file.allow=always", "submodule", "-q", "add", lib, "vendor/"+name)
	}
	testutil.Git(t, s.work, "commit", "-qm", "add submodules")
	testutil.Git(t, root, "clone", "-q", "--bare", s.work, s.bare)
	testutil.Git(t, root, "clone", "-q", s.bare, s.clone)
	testutil.Git(t, s.clone, "config", "core.autocrlf", "false")
	return s
}

// advance commits to submodule name's upstream and publishes a
// superproject commit recording it, returning the new submodule commit.
func (s superproject) advance(t *testing.T, name string) string {
	t.Helper()
	lib := filepath.Join(s.root, "work-"+name)
	mustWrite(t, filepath.Join(lib, "README.md"), "v2\n")
	testutil.Git(t, lib, "commit", "-qam", "v2")
	testutil.Git(t, lib, "push", "-q", filepath.Join(s.root, name+".git"), "main")

	sub := filepath.Join(s.work, "vendor", name)
	testutil.Git(t, sub, "pull", "-q", "origin", "main")
	testutil.Git(t, s.work, "commit", "-qam", "bump "+name)
	testutil.Git(t, s.work, "push", "-q", s.bare, "main")
	return head(t, sub)
}

func head(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD"))
}

// populated reports whether a submodule has been cloned into its path.
func populated(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// TestOpsNeverInitializeSubmodules guards against the 1.2.0 regression:
// every op ran `git submodule update --init`, so one pull cloned each
// submodule a superproject declared, including the ones its owner had
// deliberately left uninitialized.
func TestOpsNeverInitializeSubmodules(t *testing.T) {
	ctx := context.Background()
	cases := map[string]runner.Func{
		"pull":     Pull(false),
		"sync":     Sync(false),
		"reset":    Reset(false),
		"branch":   CreateBranch("feature/x", false),
		"checkout": Checkout("main", false),
	}
	for name, op := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSuperproject(t)
			if r := op(ctx, s.clone); !r.Success || strings.Contains(r.Output, "submodule") {
				t.Fatalf("%s = %+v", name, r)
			}
			for _, sub := range []string{"a", "b"} {
				if populated(filepath.Join(s.clone, "vendor", sub)) {
					t.Errorf("%s initialized vendor/%s, which was never initialized", name, sub)
				}
			}
		})
	}
}

// TestPullUpdatesInitializedSubmodules checks that Pull moves an initialized
// submodule to the commit the superproject now records and leaves an
// uninitialized sibling alone, that skipSubmodules does neither, and that
// "submodules updated" is only claimed when one actually moved.
func TestPullUpdatesInitializedSubmodules(t *testing.T) {
	ctx := context.Background()
	s := newSuperproject(t)
	testutil.Git(t, s.clone, "-c", "protocol.file.allow=always", "submodule", "-q", "update", "--init", "vendor/a")
	subA := filepath.Join(s.clone, "vendor", "a")
	before := head(t, subA)
	want := s.advance(t, "a")

	if r := Pull(true)(ctx, s.clone); !r.Success || strings.Contains(r.Output, "submodule") {
		t.Fatalf("pull (skip-submodules) = %+v", r)
	}
	if got := head(t, subA); got != before {
		t.Errorf("skip-submodules moved vendor/a to %s", got)
	}

	if r := Pull(false)(ctx, s.clone); !r.Success || !strings.HasSuffix(r.Output, " + submodules updated") {
		t.Errorf("pull = %+v", r)
	}
	if got := head(t, subA); got != want {
		t.Errorf("vendor/a is at %s, want the recorded %s", got, want)
	}
	if populated(filepath.Join(s.clone, "vendor", "b")) {
		t.Error("pull initialized vendor/b")
	}

	if r := Pull(false)(ctx, s.clone); !r.Success || r.Output != "Already up to date." {
		t.Errorf("pull with nothing to update = %+v", r)
	}
}

func TestBranchCheckoutPush(t *testing.T) {
	ctx := context.Background()
	repo := clone(t)

	if r := CreateBranch("feature/x", false)(ctx, repo); !r.Success || r.Output != "created feature/x from main" {
		t.Errorf("branch = %+v", r)
	}
	if r := Checkout("main", false)(ctx, repo); !r.Success || r.Output != "on main" {
		t.Errorf("checkout = %+v", r)
	}
	if r := Checkout("nope", false)(ctx, repo); r.Success || !strings.HasPrefix(r.Error, "checkout: ") {
		t.Errorf("bad checkout = %+v", r)
	}
	if r := Push("msg")(ctx, repo); !r.Success || r.Output != "nothing to commit" {
		t.Errorf("empty push = %+v", r)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := Push("add new")(ctx, repo); !r.Success || r.Output != "pushed to main" {
		t.Errorf("push = %+v", r)
	}
	if out := testutil.Git(t, repo, "log", "--oneline", "origin/main", "-1"); !strings.Contains(out, "add new") {
		t.Errorf("upstream did not receive the commit: %q", out)
	}
}

func TestPushNeverCommitsJunkFiles(t *testing.T) {
	ctx := context.Background()

	// Each of these dirty states consists only of junk and must report
	// "nothing to commit", never an error. They exercise the parsing traps:
	// an untracked file at the root (the first porcelain line, whose leading
	// status column git.Run would trim), a fully-untracked directory (which
	// git status collapses to "dir/"), and a tracked-but-modified junk file.
	junkOnly := map[string]func(repo string){
		"untracked root": func(repo string) {
			mustWrite(t, filepath.Join(repo, ".DS_Store"), "x")
		},
		"untracked directory": func(repo string) {
			if err := os.MkdirAll(filepath.Join(repo, "newdir"), 0o755); err != nil {
				t.Fatal(err)
			}
			mustWrite(t, filepath.Join(repo, "newdir", ".DS_Store"), "x")
		},
		"tracked and modified": func(repo string) {
			mustWrite(t, filepath.Join(repo, ".DS_Store"), "v1")
			testutil.Git(t, repo, "add", "-f", ".DS_Store")
			testutil.Git(t, repo, "commit", "-qm", "add junk")
			mustWrite(t, filepath.Join(repo, ".DS_Store"), "v2")
		},
	}
	for label, setup := range junkOnly {
		t.Run(label, func(t *testing.T) {
			repo := clone(t)
			setup(repo)
			if r := Push("junk only")(ctx, repo); !r.Success || !strings.HasPrefix(r.Output, "nothing to commit (only ") {
				t.Errorf("%s: push = %+v", label, r)
			}
		})
	}

	// Junk mixed with a real change, at the root and nested: the real file
	// is pushed, the junk is left untracked.
	repo := clone(t)
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(repo, ".DS_Store"), "x")
	mustWrite(t, filepath.Join(repo, "sub", ".DS_Store"), "x")
	mustWrite(t, filepath.Join(repo, "sub", "real.txt"), "content")
	if r := Push("real change")(ctx, repo); !r.Success || r.Output != "pushed to main" {
		t.Errorf("mixed push = %+v", r)
	}
	tracked := testutil.Git(t, repo, "ls-files")
	if strings.Contains(tracked, ".DS_Store") {
		t.Errorf(".DS_Store was committed:\n%s", tracked)
	}
	if !strings.Contains(tracked, "sub/real.txt") {
		t.Errorf("real file missing from the commit:\n%s", tracked)
	}
	if status := testutil.Git(t, repo, "status", "--porcelain"); strings.Count(status, ".DS_Store") != 2 {
		t.Errorf("junk files should remain untracked:\n%s", status)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
