// Package ops implements the git operations gitops runs across repositories.
// Every operation is a runner.Func so it can be executed in parallel by the
// runner package and driven from both the CLI and the TUI.
package ops

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/IHaveASegway/gitops/internal/git"
	"github.com/IHaveASegway/gitops/internal/runner"
)

// syncSubmodules updates repo's submodules (git submodule update --init
// --recursive) when it declares any and skip is false. It returns a suffix
// to append to a successful op's Output; a failed update is reported as a
// warning rather than turning the whole operation into a failure, since the
// primary checkout/pull already succeeded.
func syncSubmodules(ctx context.Context, repo string, skip bool) string {
	if skip || !git.HasSubmodules(repo) {
		return ""
	}
	if _, err := git.Run(ctx, repo, "submodule", "update", "--init", "--recursive"); err != nil {
		return fmt.Sprintf(" (warning: submodule update failed: %v)", err)
	}
	return " + submodules updated"
}

// Pull checks out the default branch and fast-forwards it, then updates
// submodules unless skipSubmodules is set.
func Pull(skipSubmodules bool) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)
		branch := git.DefaultBranch(ctx, repo)

		if _, err := git.Run(ctx, repo, "checkout", branch, "--"); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("checkout %s: %v", branch, err)}
		}
		out, err := git.Run(ctx, repo, "pull", "--ff-only")
		if err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("pull: %v", err)}
		}
		if out == "" {
			out = "Already up to date."
		}
		out += syncSubmodules(ctx, repo, skipSubmodules)
		return runner.Result{Repo: name, Success: true, Output: out}
	}
}

// Sync stashes local changes (untracked files included), pulls the default
// branch, restores the stash and updates submodules unless skipSubmodules
// is set.
func Sync(skipSubmodules bool) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)
		branch := git.DefaultBranch(ctx, repo)

		status, err := git.Run(ctx, repo, "status", "--porcelain")
		if err != nil {
			// Don't silently skip the auto-stash on a failed status read: local
			// changes could be lost to the checkout below.
			return runner.Result{Repo: name, Error: fmt.Sprintf("status: %v", err)}
		}
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

		if _, err := git.Run(ctx, repo, "checkout", branch, "--"); err != nil {
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
		msg += syncSubmodules(ctx, repo, skipSubmodules)
		return runner.Result{Repo: name, Success: true, Output: msg}
	}
}

// Reset discards all local changes and untracked files, force-checks out
// the default branch, pulls and updates submodules unless skipSubmodules is
// set. It is destructive by design.
func Reset(skipSubmodules bool) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)
		branch := git.DefaultBranch(ctx, repo)

		_, _ = git.Run(ctx, repo, "checkout", ".")
		_, _ = git.Run(ctx, repo, "clean", "-fd")

		if _, err := git.Run(ctx, repo, "checkout", "-f", branch, "--"); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("checkout %s: %v", branch, err)}
		}
		out, err := git.Run(ctx, repo, "pull", "--ff-only")
		if err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("pull: %v", err)}
		}
		msg := "reset + updated " + branch
		if out == "" || out == "Already up to date." {
			msg = "reset to " + branch
		}
		msg += syncSubmodules(ctx, repo, skipSubmodules)
		return runner.Result{Repo: name, Success: true, Output: msg}
	}
}

// CreateBranch returns an operation that creates a branch from an
// up-to-date default branch and updates submodules unless skipSubmodules
// is set.
func CreateBranch(branchName string, skipSubmodules bool) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)
		base := git.DefaultBranch(ctx, repo)

		if _, err := git.Run(ctx, repo, "checkout", base, "--"); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("checkout %s: %v", base, err)}
		}
		if _, err := git.Run(ctx, repo, "pull", "--ff-only"); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("pull: %v", err)}
		}
		if _, err := git.Run(ctx, repo, "checkout", "-b", branchName); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("create branch: %v", err)}
		}
		out := fmt.Sprintf("created %s from %s", branchName, base) + syncSubmodules(ctx, repo, skipSubmodules)
		return runner.Result{Repo: name, Success: true, Output: out}
	}
}

// junkNames are OS metadata files that must never be mass-committed:
// Finder drops .DS_Store files into any directory it touches, and
// `git add -A` would push them to every repository at once.
var junkNames = []string{".DS_Store"}

// addArgs stages everything except junkNames. Built once from junkNames so
// the exclusion and its user-facing description (ExcludedJunk) never drift.
var addArgs = func() []string {
	args := []string{"add", "-A", "--", "."}
	for _, n := range junkNames {
		// Default pathspec wildcards match "/" too, so */NAME covers any depth.
		args = append(args, ":(exclude)"+n, ":(exclude)*/"+n)
	}
	return args
}()

// ExcludedJunk lists, for display in confirmation prompts, the OS junk file
// names push never stages. It is derived from junkNames so a prompt can
// never claim a different exclusion than the one push actually applies.
func ExcludedJunk() string { return strings.Join(junkNames, ", ") }

// Push returns an operation that stages everything, commits with message
// and pushes the current branch. OS junk files such as .DS_Store are never
// staged; a repository whose only changes are junk reports nothing to do.
func Push(message string) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)

		status, err := git.Run(ctx, repo, "status", "--porcelain")
		if err != nil {
			// A failed status read must not be mistaken for a clean tree, or
			// real changes would be silently left unpushed.
			return runner.Result{Repo: name, Error: fmt.Sprintf("status: %v", err)}
		}
		if status == "" {
			return runner.Result{Repo: name, Success: true, Output: "nothing to commit"}
		}
		if _, err := git.Run(ctx, repo, addArgs...); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("add: %v", err)}
		}
		// The working tree was dirty but nothing landed in the index: every
		// change was an excluded junk file. Ask the index directly rather
		// than parse porcelain, which collapses untracked dirs to "dir/" and
		// loses the first line's leading status column.
		if _, err := git.Run(ctx, repo, "diff", "--cached", "--quiet"); err == nil {
			return runner.Result{Repo: name, Success: true, Output: "nothing to commit (only " + ExcludedJunk() + ")"}
		}
		if _, err := git.Run(ctx, repo, "commit", "-m", message); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("commit: %v", err)}
		}
		branch, err := git.Run(ctx, repo, "symbolic-ref", "--short", "-q", "HEAD")
		if err != nil || branch == "" {
			return runner.Result{Repo: name, Error: "committed, but HEAD is detached — push manually"}
		}
		if _, err := git.Run(ctx, repo, "push", "-u", "origin", "--", branch); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("push: %v", err)}
		}
		return runner.Result{Repo: name, Success: true, Output: "pushed to " + branch}
	}
}

// Checkout returns an operation that switches to an existing branch and
// updates submodules unless skipSubmodules is set.
func Checkout(branchName string, skipSubmodules bool) runner.Func {
	return func(ctx context.Context, repo string) runner.Result {
		name := filepath.Base(repo)
		if _, err := git.Run(ctx, repo, "checkout", branchName, "--"); err != nil {
			return runner.Result{Repo: name, Error: fmt.Sprintf("checkout: %v", err)}
		}
		out := "on " + branchName + syncSubmodules(ctx, repo, skipSubmodules)
		return runner.Result{Repo: name, Success: true, Output: out}
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
