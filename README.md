# gitops

Mass git operations across multiple repositories. Run commands against every repo in a directory — or a filtered subset — all at once, in parallel.

Includes an interactive TUI and a headless CLI mode for scripting.

## Install

```bash
# requires Go 1.21+
go install github.com/ihaveasegway/gitops@latest
```

Or build from source:

```bash
git clone https://github.com/ihaveasegway/gitops.git
cd gitops
go build -o gitops .
sudo cp gitops /usr/local/bin/
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
| `reset` | Discard all changes, force checkout default branch, pull |
| `branch` | Create a new branch from the default branch |
| `push` | Stage all changes, commit, and push current branch |
| `checkout` | Checkout an existing branch |
| `status` | Show current branch and working tree status |

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--dir` | `-d` | Base directory containing repos (default: current directory) |
| `--repos` | `-r` | Comma-separated list of repo names to target |
| `--name` | `-n` | Branch name (for `branch` and `checkout` commands) |
| `--message` | `-m` | Commit message (for `push` command) |

### Interactive TUI

Run `gitops` with no subcommand to launch the TUI:

```bash
cd ~/Documents/GitHub/my-org
gitops
```

Navigate with arrow keys, select repos with space, and confirm with enter.

### CLI Examples

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

# Check status of everything
gitops status

# Checkout an existing branch
gitops checkout -n feature/new-thing

# Operate on a different directory
gitops pull -d ~/Documents/GitHub/other-org
```

## How It Works

- Auto-discovers git repositories (directories with a `.git` folder) in the target directory
- Detects the default branch (`main` or `master`) per repo via `origin/HEAD`
- Runs all operations in parallel using goroutines
- TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss)
- CLI powered by [urfave/cli](https://github.com/urfave/cli)

## License

MIT License. See [LICENSE](LICENSE) for details.
