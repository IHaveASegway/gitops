package format

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlural(t *testing.T) {
	cases := map[string]string{
		Plural(1, "repo"):       "1 repo",
		Plural(2, "repo"):       "2 repos",
		Plural(0, "repository"): "0 repositories",
		Plural(1, "repository"): "1 repository",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestJoinNames(t *testing.T) {
	if got := JoinNames([]string{"a", "b"}, 5); got != "a, b" {
		t.Errorf("got %q", got)
	}
	if got := JoinNames([]string{"a", "b", "c", "d"}, 2); got != "a, b, … +2" {
		t.Errorf("got %q", got)
	}
}

func TestShortenPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := ShortenPath(filepath.Join(home, "code")); got != "~"+string(filepath.Separator)+"code" {
		t.Errorf("got %q", got)
	}
	if got := ShortenPath(home); got != "~" {
		t.Errorf("got %q", got)
	}
	if got := ShortenPath("/opt/x"); got != "/opt/x" {
		t.Errorf("got %q", got)
	}
}
