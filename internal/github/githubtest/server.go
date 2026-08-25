// Package githubtest provides a fake GitHub API server for tests.
package githubtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/IHaveASegway/gitops/internal/github"
)

// Token is the bearer token the fake server accepts.
const Token = "test-token"

// NewServer serves an organization whose canonical login is org with its
// first letter upper-cased, paginating repos two per page. It also serves a
// user "someone" (the authenticated user, owning one private repo), a
// rate-limited login "limited", and 404 for everything else.
func NewServer(t testing.TB, org string, repos []github.Repo) *httptest.Server {
	t.Helper()
	canonical := strings.ToUpper(org[:1]) + org[1:]
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+Token {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"message":"Bad credentials"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		switch r.URL.Path {
		case "/users/" + org:
			_ = enc.Encode(github.Owner{Login: canonical, Type: "Organization"})
		case "/users/someone":
			_ = enc.Encode(github.Owner{Login: "someone", Type: "User"})
		case "/user":
			_ = enc.Encode(map[string]string{"login": "Someone"})
		case "/user/repos":
			_ = enc.Encode([]github.Repo{{Name: "mine", FullName: "someone/mine", Private: true}})
		case "/orgs/" + canonical + "/repos":
			if r.URL.Query().Get("type") != "all" || r.URL.Query().Get("per_page") != "100" {
				t.Errorf("unexpected query %s", r.URL.RawQuery)
			}
			page := 1
			_, _ = fmt.Sscan(r.URL.Query().Get("page"), &page)
			const per = 2
			start := min((page-1)*per, len(repos))
			end := min(len(repos), start+per)
			if end < len(repos) {
				w.Header().Set("Link", fmt.Sprintf(`<%s%s?type=all&per_page=100&page=%d>; rel="next"`, srv.URL, r.URL.Path, page+1))
			}
			_ = enc.Encode(repos[start:end])
		case "/users/limited":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
