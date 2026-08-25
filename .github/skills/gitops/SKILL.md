---
name: gitops
description: 'Use the gitops CLI to run mass git operations across multiple repositories, or to clone a whole GitHub organization with gitops init. Use when: running git pull, push, sync, reset, branch, checkout, or status across many repos at once, or when asked to clone/check out all repos of a GitHub org. ALWAYS use CLI subcommands with flags — never launch the TUI.'
---

# gitops CLI

Mass git operations across multiple repositories in parallel. The binary is at `/usr/local/bin/gitops` (build from source with `go build ./cmd/gitops`).

## CRITICAL: Always Use CLI Mode, Never TUI

Running `gitops` with no subcommand launches an interactive TUI that requires keyboard navigation (it refuses to start when stdout is not a terminal). **Always pass a subcommand.** Flags may be placed before or after positional arguments.

## Commands

### Safe Commands (read-only or non-destructive)

**status** — Branch, ahead/behind counts and working tree status for all repos. Changed files are listed on the following lines.

```bash
gitops status
gitops status -r crm,pay
```

**pull** — Checkout the default branch (main/master) and pull latest (`--ff-only`).

```bash
gitops pull
gitops pull -r crm,admin
gitops pull -d ~/Documents/GitHub/some-org
```

**checkout** — Switch to an existing branch. No data loss but will fail if there are uncommitted changes.

```bash
gitops checkout -n feature/some-branch
gitops checkout -n feature/some-branch -r crm,admin
```

**sync** — Stash uncommitted changes (including untracked files), checkout default branch, pull latest, pop stash. Safe because it preserves local changes via stash. If the stash pop has a merge conflict the repo is reported as failed and the user must resolve it manually with `git stash pop`.

```bash
gitops sync
gitops sync -r crm,field-service
```

**init --dry-run** — Show what `init` would clone without cloning anything. Always run this first (see below).

### Destructive Commands (require user confirmation before running)

**reset** — DANGEROUS. Discards ALL uncommitted changes permanently (`git checkout .` + `git clean -fd`), force-checks out the default branch, and pulls. Uncommitted work is irrecoverably lost. Always confirm with the user before running this.

```bash
gitops reset
gitops reset -r crm,admin
```

**push** — Stages all changes (`git add -A`), commits with the provided message, and pushes to the remote. This pushes to whatever branch each repo is currently on. Confirm the commit message and target repos with the user.

```bash
gitops push -m "fix: update dependencies"
gitops push -m "chore: config update" -r crm,admin
```

**branch** — Checks out the default branch, pulls latest, then creates a new branch. This modifies the branch state of every targeted repo.

```bash
gitops branch -n feature/new-thing
gitops branch -n feature/new-thing -r crm,admin,field-service
```

### Cloning an organization

**init** — Lists every repository of a GitHub org (or user) visible to the token and clones the missing ones into `<dir>/<org>/`. Re-running is safe: repos already present are skipped. Archived repos are skipped unless `--archived` is given.

Workflow:

1. `gitops init <org> --dry-run -d <base-dir>` — inspect the plan. It prints the target directory, counts (to clone / already present / archived / conflicts) and **duplicate warnings**: existing checkouts of the same org found near the target (an org folder under a different name, loose clones in the base dir, or the base dir itself being a checkout of that org). Each warning includes the exact command that adds only the missing repos to the existing checkout (`gitops init <org> -d <that-dir> --here`).
2. Show the plan (especially any warnings) to the user and confirm the target directory.
3. Run non-interactively with `-y`. If a duplicate warning was shown, prefer the suggested `--here` command; only pass `--force` when the user explicitly wants a second copy.

```bash
gitops init https://github.com/acme --dry-run -d ~/Documents/GitHub
gitops init acme -y -d ~/Documents/GitHub                 # clones into ~/Documents/GitHub/acme/
gitops init acme -y -d ~/Documents/GitHub/acme-old --here # add missing repos to an existing checkout
gitops init acme -y -r crm,admin                          # only these repos
gitops init acme -y --archived --no-forks --protocol ssh
```

Accepted org forms: `https://github.com/acme`, `github.com/acme`, `acme`, `git@github.com:acme/repo.git`, GitHub Enterprise `https://ghe.example.com/acme`. Authentication comes from `GH_TOKEN`, `GITHUB_TOKEN`, or `gh auth login`; without a token only public repos are listed.

Exit codes: `0` ok, `1` at least one repo failed, `2` init refused (needs `-y`, or duplicate detected without `--force`), `130` interrupted.

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--dir <path>` | `-d` | Base directory containing repos. Defaults to the current working directory. |
| `--repos <list>` | `-r` | Comma-separated repo folder names to target. Omit to target all discovered repos. |
| `--jobs <n>` | `-j` | Max repos processed in parallel (default 8). |
| `--name <branch>` | `-n` | Branch name, used by `branch` and `checkout`. |
| `--message <msg>` | `-m` | Commit message, used by `push`. |

`init` only: `--here`, `--protocol ssh|https`, `--archived`, `--no-forks`, `--dry-run`, `-y/--yes`, `--force`.

## Behavior Notes

- Auto-discovers repos by scanning for `.git` one level deep in the target directory (hidden directories such as `.github` included)
- Detects default branch per-repo via `origin/HEAD`, falling back to `origin/main`/`origin/master`, then local `main`/`master`
- Operations run with a bounded worker pool; output has no ANSI colors when piped
- git never prompts for credentials (`GIT_TERMINAL_PROMPT=0`); repos without access fail fast
- Any failed repo makes the command exit 1 — read the per-repo lines to see which ones
- The `--dir` flag or current working directory determines which org/folder to operate on
