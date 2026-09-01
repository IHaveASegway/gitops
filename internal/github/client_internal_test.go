package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAllowedAPIBase(t *testing.T) {
	allowed := []string{
		"https://api.github.com",
		"https://proxy.corp.example/github",
		"http://localhost:8080",
		"http://127.0.0.1:39471/api",
		"http://[::1]:8080",
	}
	for _, u := range allowed {
		if !allowedAPIBase(u) {
			t.Errorf("%q should be allowed", u)
		}
	}
	denied := []string{
		"http://api.github.com",
		"http://internal.corp.example",
		"http://169.254.169.254/latest",
		"ftp://api.github.com",
		"api.github.com",
		"://bad",
		"",
	}
	for _, u := range denied {
		if allowedAPIBase(u) {
			t.Errorf("%q should be denied", u)
		}
	}
}

func TestNewClientRejectsInsecureOverride(t *testing.T) {
	t.Setenv("GITOPS_GITHUB_API", "http://internal.example/api")
	if c := NewClient("github.com", ""); c.APIBase != "https://api.github.com" {
		t.Errorf("insecure override not ignored: APIBase = %q", c.APIBase)
	}
	t.Setenv("GITOPS_GITHUB_API", "https://proxy.example/api/")
	if c := NewClient("github.com", ""); c.APIBase != "https://proxy.example/api" {
		t.Errorf("https override rejected: APIBase = %q", c.APIBase)
	}
}

func TestPaginationRefusesForeignOrigin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://attacker.example/x?page=2>; rel="next"`)
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	c := &Client{Host: "github.com", APIBase: srv.URL, HTTP: srv.Client()}

	var out []Repo
	_, err := c.get(context.Background(), "/orgs/x/repos", &out)
	if err == nil || !strings.Contains(err.Error(), "pagination") {
		t.Fatalf("foreign Link header not refused: %v", err)
	}
}

func TestListReposSanitizesUntrustedResponses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"widgets","full_name":"attacker/elsewhere","clone_url":"file:///etc/x","ssh_url":"git@evil.example:a/b.git"}]`)
	}))
	defer srv.Close()

	owner := Owner{Login: "acme", Type: "Organization"}
	c := &Client{Host: "github.com", APIBase: srv.URL, HTTP: srv.Client()}
	repos, err := c.ListRepos(context.Background(), owner, nil)
	if err != nil || len(repos) != 1 {
		t.Fatalf("repos=%v err=%v", repos, err)
	}
	got := repos[0]
	if got.CloneURL != "" || got.SSHURL != "" || got.FullName != "acme/widgets" {
		t.Errorf("hostile URLs survived an untrusted listing: %+v", got)
	}

	// With an explicit GITOPS_GITHUB_API override the endpoint is the
	// user's own choice and its URLs are kept (tests clone file:// URLs).
	c.trusted = true
	repos, err = c.ListRepos(context.Background(), owner, nil)
	if err != nil || repos[0].CloneURL != "file:///etc/x" {
		t.Errorf("trusted listing altered: %+v err=%v", repos, err)
	}
}

func TestSanitizeRepo(t *testing.T) {
	good := Repo{
		Name:     "widgets",
		FullName: "acme/widgets",
		CloneURL: "https://github.com/acme/widgets.git",
		SSHURL:   "git@github.com:acme/widgets.git",
	}
	if got := sanitizeRepo("github.com", "acme", good); got != good {
		t.Errorf("valid repo was altered: %+v", got)
	}

	// Case differences and a non-canonical-but-valid ssh:// form are
	// accepted, but the stored URLs are always the canonical form.
	ok := sanitizeRepo("github.com", "Acme", Repo{
		Name:     "Widgets",
		FullName: "acme/widgets",
		CloneURL: "https://github.com/ACME/widgets.git",
		SSHURL:   "ssh://git@github.com/acme/widgets.git",
	})
	if ok.CloneURL != "https://github.com/Acme/Widgets.git" || ok.SSHURL != "git@github.com:Acme/Widgets.git" {
		t.Errorf("URLs not canonicalized to the resolved identity: %+v", ok)
	}

	// URLs that PARSE to the right owner/repo but carry an injection (extra
	// path segments git renormalizes, or a non-default port) must not be
	// kept verbatim — they are rebuilt canonically, dropping the injection.
	inj := sanitizeRepo("github.com", "acme", Repo{
		Name:     "widgets",
		CloneURL: "https://github.com/acme/widgets/../../evil/repo.git",
		SSHURL:   "git@github.com:acme/widgets/../../evil/repo.git",
	})
	if inj.CloneURL != "https://github.com/acme/widgets.git" || inj.SSHURL != "git@github.com:acme/widgets.git" {
		t.Errorf("path-traversal injection survived: %+v", inj)
	}
	port := sanitizeRepo("github.com", "acme", Repo{Name: "widgets", CloneURL: "https://github.com:8443/acme/widgets.git"})
	if port.CloneURL != "https://github.com/acme/widgets.git" {
		t.Errorf("port injection survived: %+v", port)
	}

	bad := []Repo{
		{Name: "widgets", CloneURL: "https://evil.example/acme/widgets.git"},
		{Name: "widgets", CloneURL: "https://github.com/other/widgets.git"},
		{Name: "widgets", CloneURL: "https://github.com/acme/other.git"},
		{Name: "widgets", CloneURL: "http://github.com/acme/widgets.git"},
		{Name: "widgets", CloneURL: "file:///etc/widgets"},
		{Name: "widgets", CloneURL: "git://github.com/acme/widgets.git"},
		{Name: "widgets", SSHURL: "git@evil.example:acme/widgets.git"},
		{Name: "widgets", SSHURL: "https://github.com/acme/widgets.git"},
		{Name: "widgets", SSHURL: "git+ssh://git@github.com/acme/other.git"},
	}
	for _, r := range bad {
		got := sanitizeRepo("github.com", "acme", r)
		if got.CloneURL != "" || got.SSHURL != "" {
			t.Errorf("unsafe URL survived: in=%+v out=%+v", r, got)
		}
	}

	// A rewritten full_name falls back to the canonical identity, so the
	// constructed clone URL cannot be steered either.
	spoofed := sanitizeRepo("github.com", "acme", Repo{Name: "widgets", FullName: "attacker/elsewhere"})
	if spoofed.FullName != "acme/widgets" {
		t.Errorf("full_name not canonicalized: %q", spoofed.FullName)
	}
	if u := spoofed.RemoteURL("github.com", "https"); u != "https://github.com/acme/widgets.git" {
		t.Errorf("fallback URL = %q", u)
	}
}
