package github_test

import (
	"context"
	"strings"
	"testing"

	"github.com/IHaveASegway/gitops/internal/github"
	"github.com/IHaveASegway/gitops/internal/github/githubtest"
)

func TestParseOwner(t *testing.T) {
	good := map[string]github.OwnerRef{
		"acme":                                      {Host: "github.com", Owner: "acme"},
		"  Acme-Corp ":                              {Host: "github.com", Owner: "Acme-Corp"},
		"github.com/acme":                           {Host: "github.com", Owner: "acme"},
		"github.com/acme/":                          {Host: "github.com", Owner: "acme"},
		"https://github.com/acme":                   {Host: "github.com", Owner: "acme"},
		"https://github.com/acme/":                  {Host: "github.com", Owner: "acme"},
		"http://GitHub.com/acme/widgets":            {Host: "github.com", Owner: "acme"},
		"https://github.com/orgs/acme/repositories": {Host: "github.com", Owner: "acme"},
		"https://github.com/users/someone":          {Host: "github.com", Owner: "someone"},
		"git@github.com:acme/widgets.git":           {Host: "github.com", Owner: "acme"},
		"ssh://git@github.com/acme/widgets.git":     {Host: "github.com", Owner: "acme"},
		"https://ghe.example.com/acme":              {Host: "ghe.example.com", Owner: "acme"},
		"ghe.example.com/acme":                      {Host: "ghe.example.com", Owner: "acme"},
		"https://github.com/acme?tab=repositories":  {Host: "github.com", Owner: "acme"},
	}
	for in, want := range good {
		got, err := github.ParseOwner(in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q: got %+v want %+v", in, got, want)
		}
	}
	for _, bad := range []string{"", "github.com", "https://github.com/", "https://github.com/orgs", "not a login", "/acme", "acme!"} {
		if _, err := github.ParseOwner(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
	if got := (github.OwnerRef{Host: "github.com", Owner: "acme"}).URL(); got != "https://github.com/acme" {
		t.Errorf("URL = %q", got)
	}
}

func TestClient(t *testing.T) {
	repos := []github.Repo{
		{Name: "zeta", FullName: "acme/zeta"},
		{Name: "Alpha", FullName: "acme/Alpha", Private: true},
		{Name: "mid", FullName: "acme/mid", Archived: true},
		{Name: "beta", FullName: "acme/beta", Fork: true},
		{Name: "omega", FullName: "acme/omega"},
	}
	srv := githubtest.NewServer(t, "acme", repos)
	t.Setenv("GITOPS_GITHUB_API", srv.URL)
	c := github.NewClient("github.com", githubtest.Token)
	if c.APIBase != srv.URL {
		t.Fatalf("APIBase = %q", c.APIBase)
	}
	ctx := context.Background()

	owner, err := c.LookupOwner(ctx, "acme")
	if err != nil {
		t.Fatal(err)
	}
	if owner.Login != "Acme" || !owner.IsOrg() {
		t.Fatalf("owner = %+v", owner)
	}
	pages := 0
	got, err := c.ListRepos(ctx, owner, func(page, total int) { pages = page })
	if err != nil {
		t.Fatal(err)
	}
	if pages != 3 || len(got) != 5 {
		t.Fatalf("pages=%d repos=%d", pages, len(got))
	}
	if got[0].Name != "Alpha" || got[4].Name != "zeta" {
		t.Errorf("not sorted case-insensitively: %v", got)
	}

	// A user account that is the authenticated user lists /user/repos.
	u, err := c.LookupOwner(ctx, "someone")
	if err != nil {
		t.Fatal(err)
	}
	mine, err := c.ListRepos(ctx, u, nil)
	if err != nil || len(mine) != 1 || mine[0].Name != "mine" {
		t.Fatalf("user repos = %v, %v", mine, err)
	}

	if _, err := c.LookupOwner(ctx, "nope"); err == nil || !strings.Contains(err.Error(), `no organization or user named "nope"`) {
		t.Errorf("404 error = %v", err)
	}
	if _, err := c.LookupOwner(ctx, "limited"); err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("rate limit error = %v", err)
	}
	c.Token = "wrong"
	if _, err := c.LookupOwner(ctx, "acme"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("401 error = %v", err)
	}
}

func TestDefaultAPIBase(t *testing.T) {
	t.Setenv("GITOPS_GITHUB_API", "")
	if c := github.NewClient("github.com", ""); c.APIBase != "https://api.github.com" {
		t.Errorf("github.com base = %q", c.APIBase)
	}
	if c := github.NewClient("ghe.example.com", ""); c.APIBase != "https://ghe.example.com/api/v3" {
		t.Errorf("GHES base = %q", c.APIBase)
	}
}

func TestRepoRemoteURL(t *testing.T) {
	r := github.Repo{FullName: "acme/x", CloneURL: "https://github.com/acme/x.git", SSHURL: "git@github.com:acme/x.git"}
	if r.RemoteURL("github.com", "ssh") != "git@github.com:acme/x.git" || r.RemoteURL("github.com", "https") != "https://github.com/acme/x.git" {
		t.Error("wrong url selection")
	}
	bare := github.Repo{FullName: "acme/x"}
	if bare.RemoteURL("ghe.io", "ssh") != "git@ghe.io:acme/x.git" || bare.RemoteURL("ghe.io", "https") != "https://ghe.io/acme/x.git" {
		t.Error("wrong fallback urls")
	}
}
