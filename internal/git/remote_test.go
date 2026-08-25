package git

import "testing"

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		in               string
		host, owner, rep string
		ok               bool
	}{
		{"https://github.com/acme/widgets.git", "github.com", "acme", "widgets", true},
		{"https://github.com/acme/widgets", "github.com", "acme", "widgets", true},
		{"https://github.com/acme/widgets/", "github.com", "acme", "widgets", true},
		{"https://ghp_secret@github.com/acme/widgets.git", "github.com", "acme", "widgets", true},
		{"https://user:pw@GitHub.com/acme/widgets.git", "github.com", "acme", "widgets", true},
		{"git@github.com:acme/widgets.git", "github.com", "acme", "widgets", true},
		{"git@github.com:/acme/widgets.git", "github.com", "acme", "widgets", true},
		{"ssh://git@github.com/acme/widgets.git", "github.com", "acme", "widgets", true},
		{"ssh://git@github.com:22/acme/widgets.git", "github.com", "acme", "widgets", true},
		{"git://github.com/acme/widgets.git", "github.com", "acme", "widgets", true},
		{"https://ghe.example.com/Acme/Widgets.git", "ghe.example.com", "Acme", "Widgets", true},
		{"file:///tmp/repo.git", "", "", "", false},
		{"/tmp/repo", "", "", "", false},
		{"./repo", "", "", "", false},
		{`C:\repos\x`, "", "", "", false},
		{"https://github.com/acme", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		ref, ok := ParseRemoteURL(c.in)
		if ok != c.ok {
			t.Errorf("%q: ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok && (ref.Host != c.host || ref.Owner != c.owner || ref.Repo != c.rep) {
			t.Errorf("%q: got %+v", c.in, ref)
		}
	}
	ref, _ := ParseRemoteURL("https://github.com/Acme/Widgets.git")
	if !ref.IsOwner("github.com", "acme") || !ref.IsRepo("GitHub.com", "ACME", "widgets") || ref.String() != "Acme/Widgets" {
		t.Error("owner/repo comparisons should be case-insensitive")
	}
}

func TestRedactURL(t *testing.T) {
	if got := RedactURL("https://ghp_abc@github.com/a/b.git"); got != "https://***@github.com/a/b.git" {
		t.Errorf("got %q", got)
	}
	if got := RedactURL("git@github.com:a/b.git"); got != "git@github.com:a/b.git" {
		t.Errorf("got %q", got)
	}
}

func TestSplitSCPLike(t *testing.T) {
	host, path, ok := SplitSCPLike("git@GitHub.com:acme/x.git")
	if !ok || host != "github.com" || path != "acme/x.git" {
		t.Errorf("got %q %q %v", host, path, ok)
	}
	for _, bad := range []string{"/tmp/x", "./x", `C:\x`, "plain"} {
		if _, _, ok := SplitSCPLike(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}
