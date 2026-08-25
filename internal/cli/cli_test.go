package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/IHaveASegway/gitops/internal/testutil"
)

func TestInterspersedArgs(t *testing.T) {
	app := newApp()
	cases := map[string]string{
		"gitops init acme --dry-run -d /x":        "gitops init --dry-run -d /x acme",
		"gitops init --dry-run acme":              "gitops init --dry-run acme",
		"gitops init acme -r a,b --yes --jobs 4":  "gitops init -r a,b --yes --jobs 4 acme",
		"gitops init acme --protocol=ssh --force": "gitops init --protocol=ssh --force acme",
		"gitops -d /base init acme --here":        "gitops -d /base init --here acme",
		"gitops init acme -- --weird":             "gitops init acme --weird",
		"gitops pull -r crm":                      "gitops pull -r crm",
		"gitops nosuch acme --flag":               "gitops nosuch acme --flag",
		"gitops --help":                           "gitops --help",
	}
	for in, want := range cases {
		got := strings.Join(interspersedArgs(app, strings.Fields(in)), " ")
		if got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestResolveRepos(t *testing.T) {
	base := t.TempDir()
	testutil.NewRepo(t, filepath.Join(base, "a"), "", false)
	testutil.NewRepo(t, filepath.Join(base, "b"), "", false)
	if err := os.MkdirAll(filepath.Join(base, "not-a-repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	repos, err := resolveRepos(base, "")
	if err != nil || len(repos) != 2 {
		t.Fatalf("discover: %v, %v", repos, err)
	}
	repos, err = resolveRepos(base, " b , a ")
	if err != nil || len(repos) != 2 || filepath.Base(repos[0]) != "b" {
		t.Fatalf("named: %v, %v", repos, err)
	}
	if _, err := resolveRepos(base, "not-a-repo"); err == nil {
		t.Error("expected an error for a non-repo")
	}
	if _, err := resolveRepos(t.TempDir(), ""); err == nil || !strings.Contains(err.Error(), "gitops init") {
		t.Errorf("empty dir error = %v", err)
	}
	if _, err := resolveBaseDir(filepath.Join(base, "missing")); err == nil {
		t.Error("expected an error for a missing base dir")
	}
	if got, err := resolveBaseDir(base); err != nil || got != base {
		t.Errorf("resolveBaseDir = %q, %v", got, err)
	}
}

func TestRunHelp(t *testing.T) {
	if err := Run([]string{"gitops", "--help"}); err != nil {
		t.Errorf("--help: %v", err)
	}
	if err := Run([]string{"gitops", "init"}); err == nil || !strings.Contains(err.Error(), "usage: gitops init") {
		t.Errorf("init without args: %v", err)
	}
}
