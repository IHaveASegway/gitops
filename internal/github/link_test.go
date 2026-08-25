package github

import "testing"

func TestParseLinkNext(t *testing.T) {
	h := `<https://api.github.com/orgs/x/repos?page=3>; rel="next", <https://api.github.com/orgs/x/repos?page=5>; rel="last"`
	if got := parseLinkNext(h); got != "https://api.github.com/orgs/x/repos?page=3" {
		t.Errorf("got %q", got)
	}
	if got := parseLinkNext(`<https://x/y?page=1>; rel="prev"`); got != "" {
		t.Errorf("got %q", got)
	}
	if got := parseLinkNext(""); got != "" {
		t.Errorf("got %q", got)
	}
}
