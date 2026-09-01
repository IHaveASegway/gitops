package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IHaveASegway/gitops/internal/testutil"
)

func TestParseDefaultBranchRef(t *testing.T) {
	cases := map[string]string{
		"refs/remotes/origin/main":        "main",
		"refs/remotes/origin/release/1.0": "release/1.0",
		"origin/master":                   "master",
		"refs/heads/main":                 "",
		"":                                "",
		"refs/remotes/origin/--force":     "", // option-shaped names are repository data
		"origin/-f":                       "",
	}
	for in, want := range cases {
		if got := parseDefaultBranchRef(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestInspectAndDefaultBranch(t *testing.T) {
	root := t.TempDir()
	upstream := testutil.NewBare(t, root, "up")
	repo := filepath.Join(root, "clone")
	testutil.Git(t, root, "clone", "-q", upstream, repo)
	ctx := context.Background()

	if b := DefaultBranch(ctx, repo); b != "main" {
		t.Errorf("default branch = %q", b)
	}
	info := Inspect(ctx, repo)
	if info.Branch != "main" || info.Detached || info.Dirty != 0 || info.Ahead != 0 || info.Behind != 0 || info.Err != "" {
		t.Errorf("clean info = %+v", info)
	}

	// One local commit ahead, one dirty file.
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", "-A")
	testutil.Git(t, repo, "commit", "-qm", "ahead")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	info = Inspect(ctx, repo)
	if info.Ahead != 1 || info.Dirty != 1 || info.Tag() != "main ↑1" {
		t.Errorf("ahead/dirty info = %+v (tag %q)", info, info.Tag())
	}

	testutil.Git(t, repo, "checkout", "-q", "--detach")
	info = Inspect(ctx, repo)
	if !info.Detached || !strings.HasPrefix(info.Tag(), "detached@") {
		t.Errorf("detached info = %+v", info)
	}
}

func TestCheckBranchName(t *testing.T) {
	if err := CheckBranchName("feature/ok-1"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
	if err := CheckBranchName("bad name"); err == nil {
		t.Error("invalid name accepted")
	}
}

func TestOriginURLScanAndDiscover(t *testing.T) {
	root := t.TempDir()
	testutil.NewRepo(t, filepath.Join(root, "org", "a"), "https://tok@github.com/Acme/a.git", false)
	testutil.NewRepo(t, filepath.Join(root, "org", "b"), "git@github.com:acme/b.git", false)
	testutil.NewRepo(t, filepath.Join(root, "org", ".github"), "https://github.com/acme/.github.git", false)
	testutil.NewRepo(t, filepath.Join(root, "loose"), "", false)
	testutil.NewRepo(t, filepath.Join(root, "org", "a", "nested"), "https://github.com/other/nested.git", false) // inside a repo: ignored
	testutil.NewRepo(t, filepath.Join(root, "deep", "x", "y"), "https://github.com/deep/y.git", false)           // depth 3

	if u, ok := OriginURL(filepath.Join(root, "org", "a")); !ok || u != "https://tok@github.com/Acme/a.git" {
		t.Errorf("OriginURL = %q, %v", u, ok)
	}
	if _, ok := OriginURL(filepath.Join(root, "loose")); ok {
		t.Error("loose repo should have no origin")
	}

	got := map[string]string{}
	for _, r := range Scan(root, 2, false) {
		rel, _ := filepath.Rel(root, r.Path)
		got[filepath.ToSlash(rel)] = "-"
		if r.HasRemote {
			got[filepath.ToSlash(rel)] = r.Remote.String()
		}
	}
	want := map[string]string{"org/a": "Acme/a", "org/b": "acme/b", "loose": "-"}
	if len(got) != len(want) {
		t.Fatalf("scan found %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
	if n := len(Scan(root, 2, true)); n != 4 {
		t.Errorf("hidden scan should find 4 repos, found %d", n)
	}
	if n := len(Scan(root, 1, false)); n != 1 {
		t.Errorf("depth 1 should only find the loose repo, found %d", n)
	}
	if n := len(Scan(root, 3, false)); n != 4 {
		t.Errorf("depth 3 should find 4 repos, found %d", n)
	}

	repos, err := Discover(filepath.Join(root, "org"))
	if err != nil || len(repos) != 3 {
		t.Errorf("Discover = %v, %v (hidden repos included)", repos, err)
	}
	if !IsRepo(filepath.Join(root, "loose")) || IsRepo(root) {
		t.Error("IsRepo mismatch")
	}
}
