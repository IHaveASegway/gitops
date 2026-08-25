package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/IHaveASegway/gitops/internal/buildinfo"
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
}

// NewClient returns a client for host. The API base URL can be overridden
// with GITOPS_GITHUB_API (used by tests and API proxies).
func NewClient(host, token string) *Client {
	base := "https://api.github.com"
	if !strings.EqualFold(host, "github.com") {
		base = "https://" + host + "/api/v3"
	}
	if v := os.Getenv("GITOPS_GITHUB_API"); v != "" {
		base = strings.TrimRight(v, "/")
	}
	return &Client{Host: host, APIBase: base, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

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
	return parseLinkNext(resp.Header.Get("Link")), nil
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
		all = append(all, batch...)
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
