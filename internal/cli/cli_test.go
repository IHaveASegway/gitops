package cli

import (
	"errors"
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
	// Names must stay inside the base directory: reset/push must not be
	// steerable at repositories elsewhere on disk.
	testutil.NewRepo(t, filepath.Join(base, "nested", "inner"), "", false)
	for _, name := range []string{"../a", "..", ".", "nested/inner", `nested\inner`, "/abs", "../", "a/../b"} {
		if _, err := resolveRepos(base, name); err == nil || !strings.Contains(err.Error(), "invalid repo name") {
			t.Errorf("%q: expected an invalid-name error, got %v", name, err)
		}
	}
	// A single trailing slash (shell tab-completion) is tolerated.
	if repos, err := resolveRepos(base, "a/"); err != nil || len(repos) != 1 || filepath.Base(repos[0]) != "a" {
		t.Errorf("trailing slash: %v, %v", repos, err)
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

func TestConfirmDestructive(t *testing.T) {
	names := []string{"a", "b"}
	if ok, err := confirmDestructive("reset", "warning", names, true); !ok || err != nil {
		t.Errorf("--yes: ok=%v err=%v", ok, err)
	}
	// The test process has no TTY on stdin, so without --yes the command
	// must refuse with the "refused" exit code instead of prompting.
	ok, err := confirmDestructive("reset", "warning", names, false)
	if ok || err == nil || !strings.Contains(err.Error(), "refusing to reset") {
		t.Errorf("non-interactive: ok=%v err=%v", ok, err)
	}
	var coder interface{ ExitCode() int }
	if !errors.As(err, &coder) || coder.ExitCode() != exitRefused {
		t.Errorf("expected exit code %d, got %v", exitRefused, err)
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
