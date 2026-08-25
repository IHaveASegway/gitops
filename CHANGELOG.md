# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- `gitops init <org>`: clone every repository of a GitHub organization or user
  into `<dir>/<org>/`. Re-running clones only what is missing. Detects existing
  checkouts of the same org nearby (differently named folders, loose clones,
  or running from inside a checkout) and refuses to duplicate them unless
  asked, suggesting the `--here` command that tops up the existing checkout
  instead. Flags: `--dry-run`, `--here`, `-r`, `--protocol`, `--archived`,
  `--no-forks`, `-y`, `--force`, `-j`.
- TUI: `init` flow with a plan checklist and duplicate warnings; live branch /
  ahead-behind / dirty state in the repo picker; filter (`/`), select all /
  none / invert; confirmation screens for `reset`, `push` and `init`; live
  progress with per-repo state and cancellation; results view with expandable
  output, failures-only filter and return-to-menu; results are re-printed to
  the terminal on exit; mouse wheel scrolling.
- `--jobs/-j` to bound parallelism (default 8); Ctrl-C now kills in-flight
  git processes.
- `status` shows ahead/behind counts and lists changed files.
- Commands exit 1 when any repository fails; `init` exits 2 when it refuses
  to clone.

### Changed
- Module path is `github.com/IHaveASegway/gitops`; the binary lives in
  `cmd/gitops` and the code is split into `internal/` packages. Requires
  Go 1.27.
- `sync` stashes untracked files as well and reports a conflicting stash pop
  as a failure.
- Hidden repositories such as `.github` are discovered.
- git never prompts for credentials (`GIT_TERMINAL_PROMPT=0`); colors are
  disabled when output is not a terminal or `NO_COLOR` is set.
- Menu order is pull, sync, status, branch, checkout, push, reset, init;
  destructive entries are marked.

### Fixed
- Default-branch detection mangled branch names containing slashes and
  ignored `origin/main` / `origin/master` when no local branch existed.
- Deselecting every repository in the TUI silently meant *all* repositories.
- Misaligned columns in the TUI for the highlighted row and for names longer
  than 28 characters; truncation could split multi-byte characters.

## [0.1.0] - 2026-03-27

### Added
- Initial release: `pull`, `sync`, `reset`, `branch`, `push`, `checkout` and
  `status` across every repository in a directory, with an interactive TUI.

[Unreleased]: https://github.com/IHaveASegway/gitops/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/IHaveASegway/gitops/releases/tag/v0.1.0
