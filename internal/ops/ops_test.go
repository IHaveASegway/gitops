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
	root := t.TempDir()
	up := testutil.NewBare(t, root, "up")
	p := filepath.Join(root, "clone")
	testutil.Git(t, root, "clone", "-q", up, p)
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
	if r := Sync(ctx, repo); !r.Success || r.Output != "already up to date + stash restored" {
		t.Errorf("sync = %+v", r)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(data) != "changed" {
		t.Error("sync did not restore the local change")
	}
	if r := Reset(ctx, repo); !r.Success || r.Output != "reset to main" {
		t.Errorf("reset = %+v", r)
	}
	if data, _ := os.ReadFile(filepath.Join(repo, "README.md")); string(data) != "hi\n" {
		t.Error("reset did not discard the local change")
	}
	if r := Pull(ctx, repo); !r.Success || r.Output != "Already up to date." {
		t.Errorf("pull = %+v", r)
	}
}

func TestBranchCheckoutPush(t *testing.T) {
	ctx := context.Background()
	repo := clone(t)

	if r := CreateBranch("feature/x")(ctx, repo); !r.Success || r.Output != "created feature/x from main" {
		t.Errorf("branch = %+v", r)
	}
	if r := Checkout("main")(ctx, repo); !r.Success || r.Output != "on main" {
		t.Errorf("checkout = %+v", r)
	}
	if r := Checkout("nope")(ctx, repo); r.Success || !strings.HasPrefix(r.Error, "checkout: ") {
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
