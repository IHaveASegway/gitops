package clone

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/IHaveASegway/gitops/internal/github"
	"github.com/IHaveASegway/gitops/internal/github/githubtest"
	"github.com/IHaveASegway/gitops/internal/testutil"
)

// orgLayout builds base/acme/<names> as checkouts of acme repositories, each
// with its commit recorded on a remote-tracking ref so it starts with no
// local-only work. Tests that need a blocker introduce one explicitly.
func orgLayout(t *testing.T, names ...string) string {
	t.Helper()
	base := t.TempDir()
	for _, n := range names {
		dir := filepath.Join(base, "acme", n)
		testutil.NewRepo(t, dir, "https://github.com/acme/"+n+".git", true)
		testutil.Git(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	}
	return base
}

func orphanNames(orphans []Orphan) []string {
	out := make([]string, len(orphans))
	for i, o := range orphans {
		out[i] = o.Name
	}
	return out
}

func TestFindOrphansComparesAgainstTheUnfilteredListing(t *testing.T) {
	// gone is on disk but not in the org; archived and forked are on the
	// remote and only skipped by the filters, so neither is an orphan.
	base := orgLayout(t, "live", "gone", "archived", "forked")
	repos := []github.Repo{
		{Name: "live", FullName: "acme/live"},
		{Name: "archived", FullName: "acme/archived", Archived: true},
		{Name: "forked", FullName: "acme/forked", Fork: true},
	}
	p := plan(base, false, repos, Filter{SkipForks: true})

	if got := orphanNames(p.Orphans); len(got) != 1 || got[0] != "gone" {
		t.Errorf("orphans = %v, want [gone] — a filtered repo is still on the remote", got)
	}
}

func TestFindOrphansIsSkippedForAPartialListing(t *testing.T) {
	// With --repos the listing describes a subset, so nothing can be said
	// about what is missing; reporting orphans here would name live repos.
	base := orgLayout(t, "a", "b", "c")
	repos := []github.Repo{{Name: "a", FullName: "acme/a"}, {Name: "b", FullName: "acme/b"}, {Name: "c", FullName: "acme/c"}}
	p := plan(base, false, repos, Filter{Only: map[string]bool{"a": true}})

	if len(p.Orphans) != 0 {
		t.Errorf("orphans = %v, want none when --repos narrows the listing", orphanNames(p.Orphans))
	}
}

func TestFindOrphansIgnoresRepositoriesOfOtherOwners(t *testing.T) {
	base := t.TempDir()
	testutil.NewRepo(t, filepath.Join(base, "acme", "theirs"), "https://github.com/someone-else/theirs.git", true)
	testutil.NewRepo(t, filepath.Join(base, "acme", "no-remote"), "", true)
	p := plan(base, false, []github.Repo{{Name: "live", FullName: "acme/live"}}, Filter{})

	if len(p.Orphans) != 0 {
		t.Errorf("orphans = %v, want none: neither belongs to acme", orphanNames(p.Orphans))
	}
}

// verifyFixture builds a checkout layout, points the client at the stub API
// and returns the verified orphans keyed by name.
func verifyFixture(t *testing.T, onDisk []string, remote []github.Repo) map[string]Orphan {
	t.Helper()
	base := orgLayout(t, onDisk...)
	srv := githubtest.NewServer(t, "acme", remote)
	t.Setenv("GITOPS_GITHUB_API", srv.URL)
	c := github.NewClient("github.com", githubtest.Token)

	p := plan(base, false, remote, Filter{})
	verified := VerifyOrphans(context.Background(), c, "Acme", p.Orphans, 4)
	out := map[string]Orphan{}
	for _, o := range verified {
		out[o.Name] = o
	}
	return out
}

func TestVerifyOrphansOnlyA404MeansGone(t *testing.T) {
	// deleted-x is absent everywhere; renamed-x still resolves under a new
	// name; forbidden-x answers 403, which proves nothing either way.
	onDisk := []string{"deleted-x", "renamed-x", "forbidden-x"}
	got := verifyFixture(t, onDisk, []github.Repo{{Name: "live", FullName: "acme/live"}})

	if len(got) != 3 {
		t.Fatalf("verified %d orphans, want 3: %v", len(got), got)
	}
	if o := got["deleted-x"]; o.Status != OrphanGone || !o.Removable() {
		t.Errorf("deleted-x = %+v, want gone and removable", o)
	}
	if o := got["renamed-x"]; o.Status != OrphanRenamed || o.Removable() {
		t.Errorf("renamed-x = %+v, want renamed and NOT removable", o)
	} else if o.NewName != "Acme/new-renamed-x" {
		t.Errorf("renamed-x new name = %q", o.NewName)
	}
	if o := got["forbidden-x"]; o.Status != OrphanUnverified || o.Removable() {
		t.Errorf("forbidden-x = %+v, want unverified and NOT removable", o)
	}
}

func TestVerifyOrphansWithoutAClientConcludesNothing(t *testing.T) {
	base := orgLayout(t, "gone")
	p := plan(base, false, []github.Repo{{Name: "live", FullName: "acme/live"}}, Filter{})
	got := VerifyOrphans(context.Background(), nil, "acme", p.Orphans, 2)

	if len(got) != 1 || got[0].Status != OrphanUnverified || got[0].Removable() {
		t.Errorf("got %+v, want a single unverified, non-removable orphan", got)
	}
}

func TestLocalOnlyWorkBlocksRemoval(t *testing.T) {
	// Each of these checkouts is confirmed gone, but holds something that
	// exists nowhere else and would be destroyed with it.
	base := orgLayout(t, "dirty", "unpushed", "stashed", "extra-remote", "clean")
	dir := func(n string) string { return filepath.Join(base, "acme", n) }

	testutil.Git(t, dir("unpushed"), "commit", "-qm", "later", "--allow-empty")

	if err := os.WriteFile(filepath.Join(dir("dirty"), "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir("stashed"), "README.md"), []byte("stash me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, dir("stashed"), "stash", "push", "-q")

	testutil.Git(t, dir("extra-remote"), "remote", "add", "backup", "https://example.com/backup.git")

	srv := githubtest.NewServer(t, "acme", nil)
	t.Setenv("GITOPS_GITHUB_API", srv.URL)
	c := github.NewClient("github.com", githubtest.Token)
	p := plan(base, false, []github.Repo{{Name: "live", FullName: "acme/live"}}, Filter{})
	verified := VerifyOrphans(context.Background(), c, "Acme", p.Orphans, 4)

	for _, o := range verified {
		if o.Status != OrphanGone {
			t.Fatalf("%s: status %v, want gone", o.Name, o.Status)
		}
		if o.Name == "clean" {
			if !o.Removable() {
				t.Errorf("clean: blocked by %v, want removable", o.Blockers)
			}
			continue
		}
		if o.Removable() {
			t.Errorf("%s: removable, want blocked by local-only work", o.Name)
		}
	}
	if n := len(Removable(verified)); n != 1 {
		t.Errorf("Removable() returned %d, want only the clean one", n)
	}
}

func TestRemoveRefusesAnythingItCannotReVerify(t *testing.T) {
	base := orgLayout(t, "gone", "live")
	ctx := context.Background()
	goneDir := filepath.Join(base, "acme", "gone")

	// Status must be OrphanGone: an unverified orphan is never removed even
	// if it is otherwise clean.
	if err := Remove(ctx, "github.com", "acme", Orphan{Name: "gone", Dir: goneDir, Status: OrphanUnverified}); err == nil {
		t.Error("removed an unverified orphan")
	}
	// The origin remote must still name the repository verified as gone, so
	// a stale plan cannot delete a directory that now holds something else.
	if err := Remove(ctx, "github.com", "acme", Orphan{Name: "gone", Dir: filepath.Join(base, "acme", "live"), Status: OrphanGone}); err == nil {
		t.Error("removed a checkout whose origin names a different repo")
	}
	if err := Remove(ctx, "github.com", "acme", Orphan{Name: "gone", Dir: filepath.Join(base, "nope"), Status: OrphanGone}); err == nil {
		t.Error("removed a path that is not a repository")
	}
	if err := Remove(ctx, "github.com", "acme", Orphan{Name: "gone", Dir: "relative/path", Status: OrphanGone}); err == nil {
		t.Error("removed a relative path")
	}

	// Local-only work is re-checked at the moment of removal, not trusted
	// from the plan: an Orphan with no recorded blockers is still refused.
	if err := os.WriteFile(filepath.Join(goneDir, "README.md"), []byte("changed after planning\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Remove(ctx, "github.com", "acme", Orphan{Name: "gone", Dir: goneDir, Status: OrphanGone}); err == nil {
		t.Error("removed a checkout that became dirty after the plan was built")
	}
	if _, err := os.Stat(goneDir); err != nil {
		t.Errorf("checkout was deleted despite being refused: %v", err)
	}

	// Clean again: the removal goes through.
	testutil.Git(t, goneDir, "checkout", "-q", "--", "README.md")
	if err := Remove(ctx, "github.com", "acme", Orphan{Name: "gone", Dir: goneDir, Status: OrphanGone}); err != nil {
		t.Fatalf("clean removal failed: %v", err)
	}
	if _, err := os.Stat(goneDir); !os.IsNotExist(err) {
		t.Errorf("checkout still present after removal: %v", err)
	}
}
