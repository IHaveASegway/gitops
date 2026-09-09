package clone

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/github"
	"github.com/IHaveASegway/gitops/internal/runner"
)

// VerifyOrphans decides, for each candidate, whether it is really gone from
// the remote, and what local-only work removing it would destroy.
//
// This is the step that makes pruning safe. Absence from the organization
// listing is ambiguous — a rename, a transfer, an archived repository under
// the default filters, or a token that lost visibility all look identical to
// a deletion — so each candidate is asked for by name. Only an explicit 404
// is treated as gone. Anything else (200, a permission error, a rate limit,
// no network, no token) leaves the orphan unverified and therefore not
// removable.
func VerifyOrphans(ctx context.Context, c *github.Client, owner string, orphans []Orphan, jobs int) []Orphan {
	if len(orphans) == 0 {
		return nil
	}
	out := make([]Orphan, len(orphans))
	copy(out, orphans)
	targets := make([]string, len(out))
	index := make(map[string]int, len(out))
	for i, o := range out {
		targets[i] = o.Dir
		index[o.Dir] = i
	}
	// Each worker touches only its own element of out, so the writes do not
	// overlap and need no lock.
	runner.Run(ctx, targets, func(ctx context.Context, target string) runner.Result {
		i := index[target]
		out[i] = verifyOne(ctx, c, owner, out[i])
		return runner.Result{Success: true}
	}, jobs, nil)
	return out
}

func verifyOne(ctx context.Context, c *github.Client, owner string, o Orphan) Orphan {
	// Local state first: it is needed for every status, and it is the half
	// that does not depend on the network being reachable.
	o.Blockers = git.LocalOnlyWork(ctx, o.Dir)

	if c == nil {
		o.Status, o.Detail = OrphanUnverified, "not signed in to GitHub"
		return o
	}
	repo, err := c.LookupRepo(ctx, owner, o.Name)
	if err == nil {
		if !strings.EqualFold(repo.FullName, owner+"/"+o.Name) {
			o.Status, o.NewName = OrphanRenamed, repo.FullName
		} else {
			o.Status = OrphanPresent
		}
		return o
	}
	var ae *github.APIError
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		o.Status = OrphanGone
		return o
	}
	o.Status, o.Detail = OrphanUnverified, err.Error()
	return o
}

// Removable returns the orphans that are confirmed gone and hold no
// local-only work.
func Removable(orphans []Orphan) []Orphan {
	var out []Orphan
	for _, o := range orphans {
		if o.Removable() {
			out = append(out, o)
		}
	}
	return out
}

// Remove deletes an orphan's checkout. It re-checks the two invariants that
// make deletion safe immediately before deleting, because the plan was built
// earlier and a working tree can change under it: the path must still be a
// git repository whose origin names the repository we verified as gone, and
// it must still hold no local-only work.
func Remove(ctx context.Context, host, owner string, o Orphan) error {
	if o.Status != OrphanGone {
		return fmt.Errorf("%s: not confirmed gone from %s", o.Name, host)
	}
	if !filepath.IsAbs(o.Dir) {
		return fmt.Errorf("%s: refusing to remove a relative path %q", o.Name, o.Dir)
	}
	if !git.IsRepo(o.Dir) {
		return fmt.Errorf("%s: %s is not a git repository", o.Name, o.Dir)
	}
	u, ok := git.OriginURL(o.Dir)
	if !ok {
		return fmt.Errorf("%s: no origin remote to identify it by", o.Name)
	}
	ref, ok := git.ParseRemoteURL(u)
	if !ok || !ref.IsRepo(host, owner, o.Name) {
		return fmt.Errorf("%s: origin is %s, not %s/%s", o.Name, git.RedactURL(u), owner, o.Name)
	}
	if blockers := git.LocalOnlyWork(ctx, o.Dir); len(blockers) > 0 {
		return fmt.Errorf("%s: %s", o.Name, strings.Join(blockers, ", "))
	}
	return os.RemoveAll(o.Dir)
}
