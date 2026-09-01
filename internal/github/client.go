package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/IHaveASegway/gitops/internal/buildinfo"
	"github.com/IHaveASegway/gitops/internal/git"
)

// Owner is the resolved account as reported by the API.
type Owner struct {
	Login string `json:"login"`
	Type  string `json:"type"` // "Organization" or "User"
}

// IsOrg reports whether the account is an organization.
func (o Owner) IsOrg() bool { return o.Type == "Organization" }

// Repo is the subset of the repository object gitops uses.
type Repo struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	Fork          bool   `json:"fork"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
}

// sanitizeRepo hardens a repository object received from the API. The
// server controls full_name, clone_url and ssh_url; a hostile or proxied
// API could point them at a different repository, host, port or protocol.
// Every field is rebuilt from the host/owner/name gitops resolved itself:
// a URL that does not parse to that exact identity is discarded, and one
// that does is rewritten to its canonical form rather than kept verbatim —
// so extra path segments, dot-segments (which git renormalizes) or a
// non-default port cannot survive and steer the clone elsewhere.
func sanitizeRepo(host, owner string, r Repo) Repo {
	r.FullName = owner + "/" + r.Name
	// default_branch is display-only (a label after "cloned"); a hostile
	// value with control characters could rewrite or forge terminal output,
	// so drop it if it is not printable.
	if hasControl(r.DefaultBranch) {
		r.DefaultBranch = ""
	}
	if isRepoURL(r.CloneURL, "https", host, owner, r.Name) {
		r.CloneURL = "https://" + host + "/" + r.FullName + ".git"
	} else {
		r.CloneURL = ""
	}
	if isRepoURL(r.SSHURL, "ssh", host, owner, r.Name) {
		r.SSHURL = "git@" + host + ":" + r.FullName + ".git"
	} else {
		r.SSHURL = ""
	}
	return r
}

// hasControl reports whether s contains an ASCII control character (which
// could carry ANSI escape sequences into terminal output).
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// isRepoURL reports whether raw is an https or ssh URL that resolves to
// host/owner/name. It is only a gate: the caller rebuilds the canonical URL
// rather than trusting raw, because ParseRemoteURL reads only the first two
// path segments and ignores dot-segments and ports that git would act on.
func isRepoURL(raw, kind, host, owner, name string) bool {
	if raw == "" {
		return false
	}
	lower := strings.ToLower(raw)
	switch kind {
	case "https":
		if !strings.HasPrefix(lower, "https://") {
			return false
		}
	case "ssh":
		if strings.Contains(lower, "://") &&
			!strings.HasPrefix(lower, "ssh://") && !strings.HasPrefix(lower, "git+ssh://") && !strings.HasPrefix(lower, "ssh+git://") {
			return false
		}
	}
	ref, ok := git.ParseRemoteURL(raw)
	return ok && ref.IsRepo(host, owner, name)
}

// RemoteURL picks the URL to clone from for the requested protocol
// ("ssh" or "https").
func (r Repo) RemoteURL(host, protocol string) string {
	if protocol == "ssh" {
		if r.SSHURL != "" {
			return r.SSHURL
		}
		return "git@" + host + ":" + r.FullName + ".git"
	}
	if r.CloneURL != "" {
		return r.CloneURL
	}
	return "https://" + host + "/" + r.FullName + ".git"
}

// Client is a minimal GitHub REST client.
type Client struct {
	Host    string
	APIBase string
	Token   string
	HTTP    *http.Client

	// trusted is set when the API base was explicitly overridden with
	// GITOPS_GITHUB_API: the user chose that endpoint, so repository URLs
	// it reports are taken at face value (tests clone file:// URLs this
	// way). Responses from a host gitops picked on its own are sanitized.
	trusted bool
}

// DefaultAPIBase returns the REST API base URL for a host.
func DefaultAPIBase(host string) string {
	if strings.EqualFold(host, "github.com") {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

// NewClient returns a client for host. The API base URL can be overridden
// with GITOPS_GITHUB_API (used by tests and trusted API proxies). The token
// is sent as a Bearer header to whatever the base names, so the override
// must be https — or http on localhost only — and is otherwise ignored.
func NewClient(host, token string) *Client {
	base, trusted := DefaultAPIBase(host), false
	if v := os.Getenv("GITOPS_GITHUB_API"); v != "" {
		if allowedAPIBase(v) {
			base, trusted = strings.TrimRight(v, "/"), true
		} else {
			fmt.Fprintf(os.Stderr, "warning: ignoring GITOPS_GITHUB_API=%q: must be an https:// URL (http is allowed for localhost only)\n", v)
		}
	}
	return &Client{Host: host, APIBase: base, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}, trusted: trusted}
}

// allowedAPIBase reports whether raw is acceptable as an API base override:
// https anywhere, http only on the local machine.
func allowedAPIBase(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch u.Scheme {
	case "https":
		return true
	case "http":
		if strings.EqualFold(u.Hostname(), "localhost") {
			return true
		}
		ip := net.ParseIP(u.Hostname())
		return ip != nil && ip.IsLoopback()
	}
	return false
}

// Overridden reports whether the API base came from GITOPS_GITHUB_API. When
// it is set, the token is sent to that endpoint and its repository URLs are
// taken at face value, so callers should make the override visible.
func (c *Client) Overridden() bool { return c.trusted }

// APIError is a non-2xx response from the API.
type APIError struct {
	Status      int
	Message     string
	RateLimited bool
}

func (e *APIError) Error() string {
	switch {
	case e.RateLimited:
		return "GitHub API rate limit exceeded; authenticate (gh auth login or GITHUB_TOKEN) or wait and retry"
	case e.Status == http.StatusUnauthorized:
		return "GitHub rejected the token (401 Unauthorized); check GITHUB_TOKEN or run gh auth login"
	case e.Message != "":
		return fmt.Sprintf("GitHub API error %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("GitHub API error %d", e.Status)
}

// get performs a GET for a path ("/orgs/x") or an absolute URL and decodes
// the JSON body into out. It returns the URL of the next page, if any.
func (c *Client) get(ctx context.Context, target string, out any) (string, error) {
	if strings.HasPrefix(target, "/") {
		target = c.APIBase + target
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "gitops/"+buildinfo.Version())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		apiErr := &APIError{Status: resp.StatusCode}
		var payload struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &payload) == nil {
			apiErr.Message = payload.Message
		}
		if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
			resp.Header.Get("X-RateLimit-Remaining") == "0" {
			apiErr.RateLimited = true
		}
		return "", apiErr
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return "", fmt.Errorf("cannot decode GitHub API response: %w", err)
		}
	}
	next := parseLinkNext(resp.Header.Get("Link"))
	if next != "" && !c.sameAPIOrigin(next) {
		// The server controls the Link header; following it blindly would
		// send the token (and the request) wherever the server points.
		return "", fmt.Errorf("GitHub API pagination points outside %s; refusing to follow it", c.APIBase)
	}
	return next, nil
}

// sameAPIOrigin reports whether an absolute URL shares the API base's
// scheme, host and port. Default ports are normalized, so a proxy that
// spells an equivalent authority (an explicit :443 on https) is not falsely
// rejected mid-listing.
func (c *Client) sameAPIOrigin(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	base, err := url.Parse(c.APIBase)
	if err != nil {
		return false
	}
	return u.Scheme == base.Scheme &&
		strings.EqualFold(u.Hostname(), base.Hostname()) &&
		normalizedPort(u) == normalizedPort(base)
}

// normalizedPort returns u's port, filling in the scheme's default.
func normalizedPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch u.Scheme {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

// parseLinkNext extracts the rel="next" URL from a Link header.
func parseLinkNext(header string) string {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		if len(fields) < 2 {
			continue
		}
		u := strings.TrimSpace(fields[0])
		if !strings.HasPrefix(u, "<") || !strings.HasSuffix(u, ">") {
			continue
		}
		for _, param := range fields[1:] {
			if strings.TrimSpace(param) == `rel="next"` {
				return strings.Trim(u, "<>")
			}
		}
	}
	return ""
}

// LookupOwner resolves a login to its canonical spelling and account type.
func (c *Client) LookupOwner(ctx context.Context, login string) (Owner, error) {
	var o Owner
	if _, err := c.get(ctx, "/users/"+url.PathEscape(login), &o); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
			return o, fmt.Errorf("no organization or user named %q on %s", login, c.Host)
		}
		return o, err
	}
	// The canonical login from the API replaces the user's input and is then
	// used verbatim as a directory name (the clone target) and as the trust
	// anchor for repository URLs. A real GitHub login always matches loginRE;
	// anything else (a spoofed or MITM'd response with "../.." or a slash)
	// must not become a path or be used to rebuild clone URLs.
	if !loginRE.MatchString(o.Login) {
		return o, fmt.Errorf("%s returned an invalid account name %q", c.Host, o.Login)
	}
	return o, nil
}

// CurrentUser returns the login of the authenticated user, or "" without a token.
func (c *Client) CurrentUser(ctx context.Context) (string, error) {
	if c.Token == "" {
		return "", nil
	}
	var u struct {
		Login string `json:"login"`
	}
	if _, err := c.get(ctx, "/user", &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

// ListRepos returns every repository of owner visible to the caller, sorted
// by name. progress, if non-nil, is called after each page.
func (c *Client) ListRepos(ctx context.Context, owner Owner, progress func(page, total int)) ([]Repo, error) {
	var next string
	if owner.IsOrg() {
		next = "/orgs/" + url.PathEscape(owner.Login) + "/repos?type=all&per_page=100"
	} else {
		me, err := c.CurrentUser(ctx)
		if err != nil {
			return nil, err
		}
		if me != "" && strings.EqualFold(me, owner.Login) {
			next = "/user/repos?affiliation=owner&per_page=100" // includes private repos
		} else {
			next = "/users/" + url.PathEscape(owner.Login) + "/repos?type=owner&per_page=100"
		}
	}

	var all []Repo
	for page := 1; next != "" && page <= 200; page++ {
		var batch []Repo
		n, err := c.get(ctx, next, &batch)
		if err != nil {
			return nil, err
		}
		for _, r := range batch {
			if !c.trusted {
				r = sanitizeRepo(c.Host, owner.Login, r)
			}
			all = append(all, r)
		}
		if progress != nil {
			progress(page, len(all))
		}
		next = n
	}
	sort.Slice(all, func(i, j int) bool {
		return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
	})
	return all, nil
}
