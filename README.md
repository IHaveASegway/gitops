# gitops

[![CI](https://github.com/IHaveASegway/gitops/actions/workflows/ci.yml/badge.svg)](https://github.com/IHaveASegway/gitops/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/IHaveASegway/gitops?sort=semver)](https://github.com/IHaveASegway/gitops/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/IHaveASegway/gitops)](https://goreportcard.com/report/github.com/IHaveASegway/gitops)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Mass git operations across multiple repositories. Run commands against every repo in a directory — or a filtered subset — all at once, in parallel.

Includes an interactive TUI, a headless CLI mode for scripting, and `gitops init` to clone an entire GitHub organization in one go.

## Install

**Homebrew** (macOS and Linux):

```bash
brew install IHaveASegway/tap/gitops
```

**Go:**

```bash
go install github.com/IHaveASegway/gitops/cmd/gitops@latest
```

Prebuilt binaries for Linux, macOS and Windows (amd64 and arm64) are on the [releases page](https://github.com/IHaveASegway/gitops/releases), with checksums.

Or build from source (Go 1.27+):

```bash
git clone https://github.com/IHaveASegway/gitops.git
cd gitops
make build            # ./gitops
sudo cp gitops /usr/local/bin/
```

## Quick start

```bash
cd ~/Documents/GitHub
gitops init https://github.com/my-org     # clones every repo you can see into ./my-org/
cd my-org
gitops                                    # interactive TUI
gitops status                             # or use subcommands directly
```

## Usage

`cd` into a directory containing multiple git repos and run `gitops` to launch the interactive TUI, or use subcommands directly.

```
gitops [command] [flags]
```

### Commands

| Command | Description |
|---------|-------------|
| `pull` | Checkout default branch and pull latest |
| `sync` | Stash uncommitted changes, checkout default branch, pull, pop stash |
| `status` | Show branch, ahead/behind counts and working tree status |
| `branch` | Create a new branch from the default branch |
| `checkout` | Checkout an existing branch |
| `push` | Stage all changes, commit, and push current branch |
| `reset` | **Destructive.** Discard all changes, force checkout default branch, pull |
| `init` | Clone every repo of a GitHub org (or user) into a subdirectory |

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--dir` | `-d` | Base directory containing repos (default: current directory) |
| `--repos` | `-r` | Comma-separated list of repo names to target |
| `--jobs` | `-j` | Max repos processed in parallel (default: 8) |
| `--name` | `-n` | Branch name (for `branch` and `checkout`) |
| `--message` | `-m` | Commit message (for `push`) |

Flags may be given before or after positional arguments (`gitops init acme --dry-run` works).

### Interactive TUI

Run `gitops` with no subcommand to launch the TUI:

```bash
cd ~/Documents/GitHub/my-org
gitops
```

The header always shows which directory you are operating on and how many repos are dirty, ahead or behind.

| View | Keys |
|------|------|
| Menu | `↑/↓` move · `enter` select · `r` rescan directory · `q` quit |
| Repo picker | `space` toggle · `a` all · `n` none · `i` invert · `/` filter · `enter` continue · `esc` back |
| Text input | `enter` confirm · `esc` back (branch names are validated with `git check-ref-format`) |
| Confirmation | `y` confirm · `esc` back — shown for `reset`, `push` and `init` |
| Running | `esc` cancel (running git processes are stopped) · `ctrl+c` cancel and quit |
| Results | `↑/↓` move · `enter` expand full output · `e` expand all · `f` failures only · `esc` back to menu · `q` quit |

The picker shows each repo's branch, ahead/behind counts (`↑1 ↓2`) and the number of changed files (`*3`). Results of every run are printed to the terminal again when you quit, so they stay in your scrollback. Mouse wheel scrolling works in all lists.

If you start `gitops` in a directory without repositories, the menu offers `init`.

### `gitops init` — clone a whole organization

```bash
cd ~/Documents/GitHub
gitops init https://github.com/acme        # → ~/Documents/GitHub/acme/<repo> for every repo
gitops init acme --dry-run                 # show the plan, clone nothing
gitops init acme -r crm,admin              # only these repos
gitops init acme --archived --no-forks     # include archived repos, skip forks
gitops init acme --protocol ssh            # clone over SSH (default: gh's git_protocol setting, else https)
```

Accepted forms: `https://github.com/acme`, `github.com/acme`, `acme`, `https://github.com/orgs/acme/repositories`, `git@github.com:acme/repo.git`, and GitHub Enterprise hosts such as `https://ghe.example.com/acme`. User accounts work the same way as organizations.

**Authentication:** `GH_TOKEN`, `GITHUB_TOKEN`, or the token stored by `gh auth login` (in that order). Without a token only public repositories are listed. For HTTPS clones the token is passed to git through a process-scoped config override — it is never written to `.git/config` or shown in the process list.

**Re-running is safe.** `init` lists the org again and clones only the repos that are missing; repos already present are skipped and reported. Archived repositories are skipped unless `--archived` is given.

**Duplicate detection.** Before cloning anything, `init` looks at the `origin` remotes of the repositories around the target directory (the base directory and its parent, two levels deep). If it finds repos of the same organization somewhere other than the target — an org folder with a different name, loose clones next to it, or the directory you are already standing in — it warns and tells you the command that adds only the missing repos to that checkout instead:

```
  Organization: aspyn-io  https://github.com/aspyn-io
  Target:       ~/Documents/GitHub/aspyn-io
  Protocol:     https
  Repos:        98 repos · 79 to clone · 19 archived skipped

  ⚠ Existing checkout of aspyn-io found at ~/Documents/GitHub/aspyn (49 repos: admin, audit, automations, beacon, communications, … +44).
    Cloning into ~/Documents/GitHub/aspyn-io would duplicate those repos.
    To add only the missing repos to that checkout instead:  gitops init aspyn-io -d ~/Documents/GitHub/aspyn --here

  Clone 79 repos into ~/Documents/GitHub/aspyn-io anyway? [y/N]
```

Interactively the default answer is *No*; with `--yes` (or without a terminal) a detected duplicate aborts with exit code 2 unless `--force` is given. `--here` clones into `--dir` itself instead of creating an `<org>` subdirectory. If a directory whose name matches the org (ignoring case) already exists, it is reused — `Acme/` and `acme/` never end up side by side.

In the TUI the same plan is shown as a checklist (pick which repos to clone) followed by the warnings; `h` switches to "clone into this directory", `u` switches to "add to the existing checkout".

| `init` flag | Description |
|-------------|-------------|
| `-d, --dir` | Base directory; the `<org>` folder is created inside it (default: cwd) |
| `--here` | Clone directly into `--dir` (no `<org>` subdirectory) |
| `-r, --repos` | Only clone these repos (comma-separated) |
| `--protocol` | `ssh` or `https` |
| `--archived` | Include archived repositories |
| `--no-forks` | Skip forked repositories |
| `--dry-run` | Print the plan and exit |
| `-y, --yes` | Skip the confirmation prompt |
| `--force` | Clone even if an existing checkout was detected |
| `-j, --jobs` | Parallel clones (default: 8) |

### CLI examples

```bash
# Pull latest on default branch for all repos in current directory
gitops pull

# Pull only specific repos
gitops pull -r crm,admin,pay

# Stash changes, pull latest, restore stash across all repos
gitops sync

# Nuke all local changes and reset to default branch
gitops reset

# Create a feature branch across multiple repos
gitops branch -n feature/new-thing -r crm,admin,field-service

# Stage, commit, and push all repos on their current branch
gitops push -m "fix: update dependencies"

# Check status of everything (multi-line output lists the changed files)
gitops status

# Checkout an existing branch
gitops checkout -n feature/new-thing

# Operate on a different directory
gitops pull -d ~/Documents/GitHub/other-org
```

### Exit codes

`0` everything succeeded · `1` at least one repository failed (details are in the output) · `2` `init` refused to clone (confirmation needed or duplicate detected) · `130` interrupted.

## How it works

- Auto-discovers git repositories (directories containing `.git`, worktrees included) one level below the target directory
- Detects the default branch per repo via `origin/HEAD`, then `origin/main` / `origin/master`, then local branches
- Runs operations with a bounded worker pool (`--jobs`); Ctrl-C kills the in-flight git processes instead of orphaning them
- git never prompts for credentials (`GIT_TERMINAL_PROMPT=0`), so a repo without access fails fast instead of hanging the batch
- Colors are disabled automatically when output is not a terminal or `NO_COLOR` is set
- TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss); CLI powered by [urfave/cli](https://github.com/urfave/cli)

## Project layout

```
cmd/gitops/        entry point
internal/cli       command line (subcommands, flags, init command)
internal/tui       interactive UI, one file per view
internal/ops       the git operations
internal/clone     init: planning, duplicate detection, cloning
internal/github    GitHub API client and org-name parsing
internal/git       running git, inspecting and discovering repositories
internal/runner    bounded-parallel executor with progress events
internal/report    CLI output
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the development workflow, testing approach and release process. Changes are tracked in [CHANGELOG.md](CHANGELOG.md); security issues go through [SECURITY.md](SECURITY.md).

## License

MIT License. See [LICENSE](LICENSE) for details.
