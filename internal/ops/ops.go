// Package ops implements the git operations gitops runs across repositories.
// Every operation is a runner.Func so it can be executed in parallel by the
// runner package and driven from both the CLI and the TUI.
package ops

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/runner"
)

// Pull checks out the default branch and fast-forwards it.
func Pull(ctx context.Context, repo string) runner.Result {
	name := filepath.Base(repo)
	branch := git.DefaultBranch(ctx, repo)

	if _, err := git.Run(ctx, repo, "checkout", branch); err != nil {
		return runner.Result{Repo: name, Error: fmt.Sprintf("checkout %s: %v", branch, err)}
	}
	out, err := git.Run(ctx, repo, "pull", "--ff-only")
	if err != nil {
		return runner.Result{Repo: name, Error: fmt.Sprintf("pull: %v", err)}
	}
	if out == "" {
		out = "Already up to date."
	}
	return runner.Result{Repo: name, Success: true, Output: out}
}

// Sync stashes local changes (untracked files included), pulls the default
// branch and restores the stash.
func Sync(ctx context.Context, repo string) runner.Result {
	name := filepath.Base(repo)
	branch := git.DefaultBranch(ctx, repo)

	status, _ := git.Run(ctx, repo, "status", "--porcelain")
	stashed := false
	if status != "" {
		if _, err := git.Run(ctx, repo, "stash", "push", "--include-untracked", "-m", "gitops-sync-auto-stash"); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("stash: %v", err)}
		}
		stashed = true
	}
	restore := func() {
		if stashed {
			_, _ = git.Run(ctx, repo, "stash", "pop")
		}
	}

	if _, err := git.Run(ctx, repo, "checkout", branch); err != nil {
		restore()
		return runner.Result{Repo: name, Error: fmt.Sprintf("checkout %s: %v", branch, err)}
	}
	pullOut, err := git.Run(ctx, repo, "pull", "--ff-only")
	if err != nil {
		restore()
		return runner.Result{Repo: name, Error: fmt.Sprintf("pull: %v", err)}
	}

	msg := "already up to date"
	if pullOut != "" && pullOut != "Already up to date." {
		msg = "updated " + branch
	}
	if stashed {
		if _, err := git.Run(ctx, repo, "stash", "pop"); err != nil {
			return runner.Result{Repo: name, Error: msg + ", but stash pop conflicted — resolve with `git stash pop` in the repo"}
		}
		msg += " + stash restored"
	}
	return runner.Result{Repo: name, Success: true, Output: msg}
}

// Reset discards all local changes and untracked files, force-checks out
// the default branch and pulls. It is destructive by design.
func Reset(ctx context.Context, repo string) runner.Result {
	name := filepath.Base(repo)
	branch := git.DefaultBranch(ctx, repo)

	_, _ = git.Run(ctx, repo, "checkout", ".")
	_, _ = git.Run(ctx, repo, "clean", "-fd")

	if _, err := git.Run(ctx, repo, "checkout", "-f", branch); err != nil {
		return runner.Result{Repo: name, Error: fmt.Sprintf("checkout %s: %v", branch, err)}
	}
	out, err := git.Run(ctx, repo, "pull", "--ff-only")
	if err != nil {
		return runner.Result{Repo: name, Error: fmt.Sprintf("pull: %v", err)}
	}
	if out == "" || out == "Already up to date." {
		return runner.Result{Repo: name, Success: true, Output: "reset to " + branch}
	}
	return runner.Result{Repo: name, Success: true, Output: "reset + updated " + branch}
}

// CreateBranch returns an operation that creates a branch from an
// up-to-date default branch.
func CreateBranch(branchName string) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)
		base := git.DefaultBranch(ctx, repo)

		if _, err := git.Run(ctx, repo, "checkout", base); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("checkout %s: %v", base, err)}
		}
		if _, err := git.Run(ctx, repo, "pull", "--ff-only"); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("pull: %v", err)}
		}
		if _, err := git.Run(ctx, repo, "checkout", "-b", branchName); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("create branch: %v", err)}
		}
		return runner.Result{Repo: name, Success: true, Output: fmt.Sprintf("created %s from %s", branchName, base)}
	}
}

// Push returns an operation that stages everything, commits with message
// and pushes the current branch.
func Push(message string) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)

		status, _ := git.Run(ctx, repo, "status", "--porcelain")
		if status == "" {
			return runner.Result{Repo: name, Success: true, Output: "nothing to commit"}
		}
		if _, err := git.Run(ctx, repo, "add", "-A"); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("add: %v", err)}
		}
		if _, err := git.Run(ctx, repo, "commit", "-m", message); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("commit: %v", err)}
		}
		branch, err := git.Run(ctx, repo, "symbolic-ref", "--short", "-q", "HEAD")
		if err != nil || branch == "" {
			return runner.Result{Repo: name, Error: "committed, but HEAD is detached — push manually"}
		}
		if _, err := git.Run(ctx, repo, "push", "-u", "origin", branch); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("push: %v", err)}
		}
		return runner.Result{Repo: name, Success: true, Output: "pushed to " + branch}
	}
}

// Checkout returns an operation that switches to an existing branch.
func Checkout(branchName string) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)
		if _, err := git.Run(ctx, repo, "checkout", branchName); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("checkout: %v", err)}
		}
		return runner.Result{Repo: name, Success: true, Output: "on " + branchName}
	}
}

// Status reports the branch, ahead/behind counts and changed files.
func Status(ctx context.Context, repo string) runner.Result {
	name := filepath.Base(repo)
	info := git.Inspect(ctx, repo)
	if info.Err != "" {
		return runner.Result{Repo: name, Error: info.Err}
	}
	summary := "clean"
	if info.Dirty > 0 {
		summary = fmt.Sprintf("%d changed", info.Dirty)
	}
	out := fmt.Sprintf("[%s] %s", info.Tag(), summary)
	if info.Dirty > 0 {
		if short, err := git.Run(ctx, repo, "status", "--short"); err == nil && short != "" {
			out += "\n" + short
		}
	}
	return runner.Result{Repo: name, Success: true, Output: out}
}
