package git

import (
	"context"
	"strings"
)

// parseDefaultBranchRef turns "refs/remotes/origin/main" into "main",
// keeping slashes inside branch names ("release/1.0") intact.
func parseDefaultBranchRef(ref string) string {
	ref = strings.TrimSpace(ref)
	for _, prefix := range []string{"refs/remotes/origin/", "origin/"} {
		if b, ok := strings.CutPrefix(ref, prefix); ok && b != "" {
			return b
		}
	}
	return ""
}

// DefaultBranch determines the repository's default branch: origin/HEAD
// first, then whichever of main/master exists on the remote, then locally.
func DefaultBranch(ctx context.Context, repo string) string {
	if out, err := Run(ctx, repo, "symbolic-ref", "refs/remotes/origin/HEAD"); err == nil {
		if b := parseDefaultBranchRef(out); b != "" {
			return b
		}
	}
	for _, prefix := range []string{"refs/remotes/origin/", "refs/heads/"} {
		for _, b := range []string{"main", "master"} {
			if _, err := Run(ctx, repo, "rev-parse", "--verify", "--quiet", prefix+b); err == nil {
				return b
			}
		}
	}
	return "main"
}
