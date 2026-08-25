package git

import (
	"net/url"
	"regexp"
	"strings"
)

// RemoteRef identifies a repository on a git host, as parsed from a remote URL.
type RemoteRef struct {
	Host  string // lowercase host name, e.g. "github.com"
	Owner string // owner/org login as written in the URL
	Repo  string // repository name without a trailing ".git"
}

// String renders the ref as "owner/repo".
func (r RemoteRef) String() string { return r.Owner + "/" + r.Repo }

// IsOwner reports whether the ref belongs to host/owner. GitHub hosts,
// logins and repository names compare case-insensitively.
func (r RemoteRef) IsOwner(host, owner string) bool {
	return strings.EqualFold(r.Host, host) && strings.EqualFold(r.Owner, owner)
}

// IsRepo reports whether the ref is exactly host/owner/repo.
func (r RemoteRef) IsRepo(host, owner, repo string) bool {
	return r.IsOwner(host, owner) && strings.EqualFold(r.Repo, repo)
}

var scpLikeRE = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s]+):(.+)$`)

// SplitSCPLike splits the classic SSH form "user@host:path" into host and
// path. Local paths and Windows drive letters are rejected.
func SplitSCPLike(raw string) (host, path string, ok bool) {
	if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, ".") {
		return "", "", false
	}
	m := scpLikeRE.FindStringSubmatch(raw)
	if m == nil || len(m[1]) == 1 {
		return "", "", false
	}
	return strings.ToLower(m[1]), m[2], true
}

// ParseRemoteURL extracts host/owner/repo from the common remote URL forms:
//
//	https://github.com/owner/repo.git
//	https://TOKEN@github.com/owner/repo.git   (credentials are discarded)
//	git@github.com:owner/repo.git
//	ssh://git@github.com/owner/repo.git
//	git://github.com/owner/repo
//
// Local paths and file:// URLs are not remote refs.
func ParseRemoteURL(raw string) (RemoteRef, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RemoteRef{}, false
	}
	var host, path string
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return RemoteRef{}, false
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ssh", "git", "git+ssh", "ssh+git":
		default:
			return RemoteRef{}, false
		}
		host, path = strings.ToLower(u.Hostname()), u.Path
	} else {
		var ok bool
		if host, path, ok = SplitSCPLike(raw); !ok {
			return RemoteRef{}, false
		}
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 2 || segs[0] == "" || segs[1] == "" {
		return RemoteRef{}, false
	}
	repo := strings.TrimSuffix(segs[1], ".git")
	if repo == "" {
		return RemoteRef{}, false
	}
	return RemoteRef{Host: host, Owner: segs[0], Repo: repo}, true
}

// RedactURL removes any userinfo (tokens, passwords) from a URL for display.
func RedactURL(raw string) string {
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 && !strings.Contains(rest[:at], "/") {
			return raw[:i+3] + "***@" + rest[at+1:]
		}
	}
	return raw
}
