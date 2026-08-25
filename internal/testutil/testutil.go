// Package testutil creates real git repositories for tests.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Git runs git in dir with a deterministic identity and no user/system
// configuration, failing the test on error.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0", "LC_ALL=C",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// NewRepo creates a repository at dir on branch "main", optionally with an
// origin remote and an initial commit.
func NewRepo(t testing.TB, dir, origin string, commit bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	if origin != "" {
		Git(t, dir, "remote", "add", "origin", origin)
	}
	if commit {
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		Git(t, dir, "add", "-A")
		Git(t, dir, "commit", "-qm", "init")
	}
}

// NewBare creates a bare repository with one commit under root and returns
// its file:// URL, usable as a clone source or upstream.
func NewBare(t testing.TB, root, name string) string {
	t.Helper()
	work := filepath.Join(root, "work-"+name)
	NewRepo(t, work, "", true)
	bare := filepath.Join(root, name+".git")
	Git(t, root, "clone", "-q", "--bare", work, bare)
	return FileURL(bare)
}

// FileURL converts a local path into a file:// URL.
func FileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}
