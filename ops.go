package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Result holds the outcome of a git operation on a single repo.
type Result struct {
	Repo    string
	Group   string
	Skipped bool
	Success bool
	Output  string
	Error   string
}

// discoverRepos scans baseDir for subdirectories containing a .git folder.
func discoverRepos(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", baseDir, err)
	}
	var repos []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		gitDir := filepath.Join(baseDir, entry.Name(), ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			repos = append(repos, filepath.Join(baseDir, entry.Name()))
		}
	}
	return repos, nil
}

// discoverReposRecursive scans one level of subdirectories under baseDir for git repos.
func discoverReposRecursive(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("cannot read directory %s: %w", baseDir, err)
	}
	var repos []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		subDir := filepath.Join(baseDir, entry.Name())
		gitDir := filepath.Join(subDir, ".git")
		if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
			repos = append(repos, subDir)
			continue
		}
		subRepos, err := discoverRepos(subDir)
		if err != nil {
			continue
		}
		repos = append(repos, subRepos...)
	}
	return repos, nil
}

// annotateGroups sets Group and Repo fields based on relative paths from baseDir.
func annotateGroups(baseDir string, repos []string, results []Result) {
	for i, repoPath := range repos {
		rel, err := filepath.Rel(baseDir, repoPath)
		if err != nil {
			continue
		}
		dir := filepath.Dir(rel)
		if dir != "." {
			results[i].Group = dir
			results[i].Repo = rel
		}
	}
}

// getDefaultBranch returns "main" or "master" depending on the repo.
func getDefaultBranch(repoPath string) string {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		parts := strings.Split(ref, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	cmd = exec.Command("git", "rev-parse", "--verify", "main")
	cmd.Dir = repoPath
	if err := cmd.Run(); err == nil {
		return "main"
	}
	return "master"
}

// runGit executes a git command in the given repo directory.
func runGit(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := strings.TrimSpace(stdout.String())
	if err != nil {
		errOut := strings.TrimSpace(stderr.String())
		if errOut == "" {
			errOut = err.Error()
		}
		return output, fmt.Errorf("%s", errOut)
	}
	return output, nil
}

// getCurrentBranch returns the currently checked-out branch name.
func getCurrentBranch(repoPath string) (string, error) {
	out, err := runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func opPull(repoPath string) Result {
	repo := filepath.Base(repoPath)
	defaultBranch := getDefaultBranch(repoPath)

	// Remember the current branch so we can switch back
	originalBranch, err := getCurrentBranch(repoPath)
	if err != nil {
		return Result{Repo: repo, Error: fmt.Sprintf("get current branch: %v", err)}
	}

	needSwitch := originalBranch != defaultBranch

	if needSwitch {
		if _, err := runGit(repoPath, "checkout", defaultBranch); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("checkout %s: %v", defaultBranch, err)}
		}
	}

	out, err := runGit(repoPath, "pull", "--ff-only")
	if err != nil {
		// Attempt to switch back even on pull failure
		if needSwitch {
			runGit(repoPath, "checkout", originalBranch)
		}
		return Result{Repo: repo, Error: fmt.Sprintf("pull: %v", err)}
	}
	if out == "" {
		out = "Already up to date."
	}

	// Switch back to the original branch
	if needSwitch {
		if _, err := runGit(repoPath, "checkout", originalBranch); err != nil {
			return Result{Repo: repo, Success: true, Output: out + fmt.Sprintf(" (warning: could not switch back to %s: %v)", originalBranch, err)}
		}
	}

	return Result{Repo: repo, Success: true, Output: out}
}

func opSync(repoPath string) Result {
	repo := filepath.Base(repoPath)
	branch := getDefaultBranch(repoPath)

	// Check if there are uncommitted changes to stash
	status, _ := runGit(repoPath, "status", "--porcelain")
	stashed := false
	if status != "" {
		if _, err := runGit(repoPath, "stash", "push", "-m", "gitops-sync-auto-stash"); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("stash: %v", err)}
		}
		stashed = true
	}

	// Checkout default branch
	if _, err := runGit(repoPath, "checkout", branch); err != nil {
		if stashed {
			runGit(repoPath, "stash", "pop")
		}
		return Result{Repo: repo, Error: fmt.Sprintf("checkout %s: %v", branch, err)}
	}

	// Pull latest
	pullOut, err := runGit(repoPath, "pull", "--ff-only")
	if err != nil {
		if stashed {
			runGit(repoPath, "stash", "pop")
		}
		return Result{Repo: repo, Error: fmt.Sprintf("pull: %v", err)}
	}

	// Pop stash if we stashed anything
	msg := "pulled"
	if pullOut != "" && pullOut != "Already up to date." {
		msg = "updated"
	}
	if stashed {
		if _, err := runGit(repoPath, "stash", "pop"); err != nil {
			return Result{Repo: repo, Success: true, Output: msg + " (stash pop conflict - run git stash pop manually)"}
		}
		msg += " + stash restored"
	}
	return Result{Repo: repo, Success: true, Output: msg}
}

func opReset(repoPath string) Result {
	repo := filepath.Base(repoPath)
	branch := getDefaultBranch(repoPath)

	// Discard all uncommitted changes
	runGit(repoPath, "checkout", ".")
	runGit(repoPath, "clean", "-fd")

	// Force checkout default branch
	if _, err := runGit(repoPath, "checkout", "-f", branch); err != nil {
		return Result{Repo: repo, Error: fmt.Sprintf("checkout %s: %v", branch, err)}
	}

	// Pull latest
	out, err := runGit(repoPath, "pull", "--ff-only")
	if err != nil {
		return Result{Repo: repo, Error: fmt.Sprintf("pull: %v", err)}
	}
	if out == "" || out == "Already up to date." {
		return Result{Repo: repo, Success: true, Output: "reset to " + branch}
	}
	return Result{Repo: repo, Success: true, Output: "reset + updated " + branch}
}

func opCreateBranch(name string) func(string) Result {
	return func(repoPath string) Result {
		repo := filepath.Base(repoPath)
		branch := getDefaultBranch(repoPath)

		if _, err := runGit(repoPath, "checkout", branch); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("checkout %s: %v", branch, err)}
		}
		if _, err := runGit(repoPath, "pull", "--ff-only"); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("pull: %v", err)}
		}
		if _, err := runGit(repoPath, "checkout", "-b", name); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("create branch: %v", err)}
		}
		return Result{Repo: repo, Success: true, Output: fmt.Sprintf("created %s from %s", name, branch)}
	}
}

func opPush(message string) func(string) Result {
	return func(repoPath string) Result {
		repo := filepath.Base(repoPath)

		status, _ := runGit(repoPath, "status", "--porcelain")
		if status == "" {
			return Result{Repo: repo, Success: true, Output: "nothing to commit"}
		}

		if _, err := runGit(repoPath, "add", "-A"); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("add: %v", err)}
		}
		if _, err := runGit(repoPath, "commit", "-m", message); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("commit: %v", err)}
		}

		branch, err := runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("get branch: %v", err)}
		}

		if _, err := runGit(repoPath, "push", "-u", "origin", branch); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("push: %v", err)}
		}
		return Result{Repo: repo, Success: true, Output: "pushed to " + branch}
	}
}

func opCheckout(name string) func(string) Result {
	return func(repoPath string) Result {
		repo := filepath.Base(repoPath)
		if _, err := runGit(repoPath, "checkout", name); err != nil {
			return Result{Repo: repo, Error: fmt.Sprintf("checkout: %v", err)}
		}
		return Result{Repo: repo, Success: true, Output: "on " + name}
	}
}

func opStatus(repoPath string) Result {
	repo := filepath.Base(repoPath)
	branch, _ := runGit(repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	status, err := runGit(repoPath, "status", "--short")
	if err != nil {
		return Result{Repo: repo, Error: err.Error()}
	}
	if status == "" {
		status = "clean"
	}
	return Result{Repo: repo, Success: true, Output: fmt.Sprintf("[%s] %s", branch, status)}
}

func runParallel(repos []string, op func(string) Result) []Result {
	results := make([]Result, len(repos))
	var wg sync.WaitGroup
	for i, repo := range repos {
		wg.Add(1)
		go func(idx int, path string) {
			defer wg.Done()
			results[idx] = op(path)
		}(i, repo)
	}
	wg.Wait()
	return results
}

func printResults(title string, results []Result, skipped int) {
	fmt.Printf("\n  %s Results\n", title)
	fmt.Printf("  %s\n", strings.Repeat("-", 58))

	grouped := false
	for _, r := range results {
		if r.Group != "" {
			grouped = true
			break
		}
	}

	successes, failures := 0, 0
	if grouped {
		groupOrder := []string{}
		groupMap := map[string][]Result{}
		for _, r := range results {
			g := r.Group
			if _, exists := groupMap[g]; !exists {
				groupOrder = append(groupOrder, g)
			}
			groupMap[g] = append(groupMap[g], r)
		}
		for _, g := range groupOrder {
			fmt.Printf("\n  \033[1m%s/\033[0m\n", g)
			for _, r := range groupMap[g] {
				name := filepath.Base(r.Repo)
				if r.Success {
					successes++
					fmt.Printf("    \033[32m+\033[0m %-26s %s\n", name, r.Output)
				} else {
					failures++
					fmt.Printf("    \033[31mx\033[0m %-26s %s\n", name, r.Error)
				}
			}
		}
	} else {
		for _, r := range results {
			if r.Success {
				successes++
				fmt.Printf("  \033[32m+\033[0m %-28s %s\n", r.Repo, r.Output)
			} else {
				failures++
				fmt.Printf("  \033[31mx\033[0m %-28s %s\n", r.Repo, r.Error)
			}
		}
	}

	fmt.Printf("\n  %s\n", strings.Repeat("-", 58))
	fmt.Printf("  Total: %d  \033[32mSuccess: %d\033[0m  \033[31mFailed: %d\033[0m  Skipped: %d\n\n",
		len(results), successes, failures, skipped)
}
