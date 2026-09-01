package github

import "testing"

func TestFindTokenIsScopedToHost(t *testing.T) {
	t.Setenv("PATH", "") // keep the gh CLI out of the fallback chain
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_ENTERPRISE_TOKEN", "")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "")

	if tok, src := FindToken("github.com"); tok != "" || src != "" {
		t.Errorf("no env: got %q from %q", tok, src)
	}

	t.Setenv("GITHUB_TOKEN", "dotcom-token")
	if tok, src := FindToken("github.com"); tok != "dotcom-token" || src != "$GITHUB_TOKEN" {
		t.Errorf("github.com: got %q from %q", tok, src)
	}
	// A github.com token must never be offered to any other host — not a
	// GHE instance, and especially not a look-alike or mistyped host.
	for _, host := range []string{"ghe.example.com", "githb.com", "github.com.evil.tld"} {
		if tok, src := FindToken(host); tok != "" {
			t.Errorf("%s: leaked github.com token %q from %q", host, tok, src)
		}
	}

	t.Setenv("GH_ENTERPRISE_TOKEN", "ghe-token")
	if tok, src := FindToken("ghe.example.com"); tok != "ghe-token" || src != "$GH_ENTERPRISE_TOKEN" {
		t.Errorf("ghe: got %q from %q", tok, src)
	}
	if tok, src := FindToken("GitHub.com"); tok != "dotcom-token" || src != "$GITHUB_TOKEN" {
		t.Errorf("github.com must not use the enterprise token: got %q from %q", tok, src)
	}

	t.Setenv("GH_TOKEN", "gh-token")
	if tok, src := FindToken("github.com"); tok != "gh-token" || src != "$GH_TOKEN" {
		t.Errorf("GH_TOKEN precedence: got %q from %q", tok, src)
	}
}
