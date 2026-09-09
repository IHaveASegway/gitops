package git

import (
	"context"
	"fmt"
	"strings"
)

// LocalOnlyWork reports everything in repo that exists only on this machine
// and would be destroyed along with it: uncommitted changes, stashes,
// commits absent from every remote, and remotes other than origin (which
// could be the only place some branch was pushed).
//
// It is the gate in front of every deletion, so it fails closed: a git
// command that errors is reported as a blocker rather than silently treated
// as "nothing here". An empty result means the checkout can be recreated
// from its remote and is safe to remove.
func LocalOnlyWork(ctx context.Context, repo string) []string {
	var reasons []string

	if out, err := Run(ctx, repo, "status", "--porcelain"); err != nil {
		reasons = append(reasons, "cannot read status: "+err.Error())
	} else if n := countLines(out); n > 0 {
		reasons = append(reasons, plural(n, "uncommitted change", "uncommitted changes"))
	}

	if out, err := Run(ctx, repo, "stash", "list"); err != nil {
		reasons = append(reasons, "cannot read stashes: "+err.Error())
	} else if n := countLines(out); n > 0 {
		reasons = append(reasons, plural(n, "stash entry", "stash entries"))
	}

	// Commits reachable from any local branch but from no remote-tracking
	// branch. This catches work on a branch that was never pushed, which a
	// plain ahead/behind count against one upstream would miss.
	if out, err := Run(ctx, repo, "log", "--branches", "--not", "--remotes", "--format=%H"); err != nil {
		reasons = append(reasons, "cannot read unpushed commits: "+err.Error())
	} else if n := countLines(out); n > 0 {
		reasons = append(reasons, plural(n, "unpushed commit", "unpushed commits"))
	}

	if out, err := Run(ctx, repo, "remote"); err != nil {
		reasons = append(reasons, "cannot read remotes: "+err.Error())
	} else {
		var extra []string
		for _, name := range strings.Fields(out) {
			if name != "origin" {
				extra = append(extra, name)
			}
		}
		if len(extra) > 0 {
			reasons = append(reasons, "other remotes: "+strings.Join(extra, ", "))
		}
	}

	return reasons
}

func countLines(out string) int {
	if strings.TrimSpace(out) == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSpace(out), "\n"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
