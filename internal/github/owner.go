// Package github talks to the GitHub REST API and resolves how an
// organization was named on the command line.
package github

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/IHaveASegway/gitops/internal/git"
)

// OwnerRef is an organization or user account on a GitHub host.
type OwnerRef struct {
	Host  string
	Owner string
}

// URL returns the browsable URL of the owner, e.g. https://github.com/acme.
func (o OwnerRef) URL() string { return "https://" + o.Host + "/" + o.Owner }

var loginRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ParseOwner accepts the many ways people write an org: a bare login
// ("acme"), "github.com/acme", any https://github.com/acme[/...] URL
// (including /orgs/acme), an SSH remote ("git@github.com:acme/repo.git"),
// or a GitHub Enterprise host ("https://ghe.example.com/acme").
func ParseOwner(input string) (OwnerRef, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return OwnerRef{}, errors.New("an organization URL or name is required")
	}
	host := "github.com"
	var path string
	switch {
	case strings.Contains(s, "://"):
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return OwnerRef{}, fmt.Errorf("cannot parse %q as a URL", s)
		}
		host, path = strings.ToLower(u.Hostname()), u.Path
	case strings.HasPrefix(s, "/"):
		return OwnerRef{}, fmt.Errorf("%q looks like a filesystem path, not a GitHub organization", s)
	default:
		if h, p, ok := git.SplitSCPLike(s); ok {
			host, path = h, p
			break
		}
		if first, rest, found := strings.Cut(s, "/"); found {
			if strings.Contains(first, ".") {
				host, path = strings.ToLower(first), rest
			} else {
				path = s
			}
			break
		}
		if strings.Contains(s, ".") {
			return OwnerRef{}, fmt.Errorf("%q looks like a host name with no organization; try %s/<org>", s, s)
		}
		path = s
	}

	var segs []string
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	if len(segs) == 0 {
		return OwnerRef{}, fmt.Errorf("no organization or user found in %q", s)
	}
	owner := segs[0]
	if owner == "orgs" || owner == "users" {
		if len(segs) < 2 {
			return OwnerRef{}, fmt.Errorf("no organization or user found in %q", s)
		}
		owner = segs[1]
	}
	owner = strings.TrimSuffix(owner, ".git")
	if !loginRE.MatchString(owner) {
		return OwnerRef{}, fmt.Errorf("%q is not a valid GitHub login", owner)
	}
	return OwnerRef{Host: NormalizeHost(host), Owner: owner}, nil
}

// NormalizeHost canonicalizes a host name so that spellings which resolve to
// the same GitHub host take the same code path. Every host decision keys off
// this string — which token applies (FindToken) and which API base is used
// (DefaultAPIBase) — so an un-normalized "www.github.com" would be treated as
// a GitHub Enterprise host: the wrong API base, and a github.com token sent
// to a host the docs promise it never reaches.
func NormalizeHost(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if rest, ok := strings.CutPrefix(host, "www."); ok && rest == "github.com" {
		host = rest
	}
	return host
}
