package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestPullUpdatesSubmodulesUnlessSkipped clones a repo whose submodule is
// registered but not yet checked out (as after a plain, non-recursive
// clone) and confirms Pull initializes it, while skipSubmodules leaves it
// alone.
func TestPullUpdatesSubmodulesUnlessSkipped(t *testing.T) {
	testutil.Identity(t)
	t.Setenv("GIT_ALLOW_PROTOCOL", "file") // the submodule's origin is a local file:// path
	ctx := context.Background()
	root := t.TempDir()

	libBare := testutil.NewBare(t, root, "lib")

	work := filepath.Join(root, "work")
	testutil.NewRepo(t, work, "", true)
	testutil.Git(t, work, "-c", "protocol.file.allow=always", "submodule", "-q", "add", libBare, "vendor/lib")
	testutil.Git(t, work, "commit", "-qm", "add submodule")
	parentBare := filepath.Join(root, "parent.git")
	testutil.Git(t, root, "clone", "-q", "--bare", work, parentBare)

	repo := filepath.Join(root, "clone")
	testutil.Git(t, root, "clone", "-q", parentBare, repo)
	testutil.Git(t, repo, "config", "core.autocrlf", "false")

	subGit := filepath.Join(repo, "vendor", "lib", ".git")

	if r := Pull(true)(ctx, repo); !r.Success || strings.Contains(r.Output, "submodule") {
		t.Fatalf("pull (skip-submodules) = %+v", r)
	}
	if _, err := os.Stat(subGit); err == nil {
		t.Error("skip-submodules should leave the submodule uninitialized")
	}

	if r := Pull(false)(ctx, repo); !r.Success || !strings.Contains(r.Output, "submodules updated") {
		t.Errorf("pull = %+v", r)
	}
	if _, err := os.Stat(subGit); err != nil {
		t.Error("submodule should be initialized after pull")
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
