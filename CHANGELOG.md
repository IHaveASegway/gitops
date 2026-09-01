# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [1.1.0] - 2026-09-01

### Security
- Tokens are now strictly scoped to their host: `GH_TOKEN`/`GITHUB_TOKEN`
  are sent to github.com only. Enterprise hosts use `GH_ENTERPRISE_TOKEN`,
  `GITHUB_ENTERPRISE_TOKEN` or `gh auth login --hostname <host>`; the
  github.com fallback for unknown hosts is gone.
- GitHub API pagination links are followed only on the same scheme and host
  the listing started on.
- Repository `clone_url`/`ssh_url`/`full_name` values reported by the API
  are validated against the resolved host/owner/name; anything else is
  discarded and the URL is rebuilt from the canonical identity.
- `GITOPS_GITHUB_API` must be an `https://` URL (`http://` for localhost
  only); the CLI prints the active base before making requests, and invalid
  values are ignored with a warning.
- CLI `branch` validates its name with `git check-ref-format`; `checkout`
  and other ref arguments reject option-shaped values (a leading `-`) so
  they cannot inject git flags, and an option-shaped `origin/HEAD` value is
  ignored. Refs are also passed after `--` where that disambiguates them
  from pathspecs.
- The account name the API resolves is re-validated before it is used as a
  clone directory or to rebuild repository URLs, so a spoofed response
  cannot direct clones outside the chosen directory.
- `govulncheck` runs in CI, and release checksums are signed with cosign
  (keyless Sigstore); the release job's GoReleaser and cosign binaries are
  version-pinned for reproducibility.
- Homebrew now publishes a formula instead of a cask, removing the
  post-install hook that stripped Gatekeeper's quarantine attribute.

### Changed
- CLI `reset` and `push` are gated like the TUI: they ask for confirmation
  in a terminal and refuse with exit code 2 without one unless `--yes`/`-y`
  is passed.
- `--repos` names must be bare directory names inside the base directory;
  path segments such as `../other` are rejected.
- Windows is covered by the CI test matrix instead of compile-only.

### Fixed
- `push` no longer commits macOS `.DS_Store` files (`git add -A` picked
  them up); a repository whose only changes are such junk files reports
  "nothing to commit".

## [1.0.0] - 2026-08-25

### Added
- Homebrew tap: `brew install IHaveASegway/tap/gitops` (published automatically by
  GoReleaser on every release, macOS and Linux).
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

[Unreleased]: https://github.com/IHaveASegway/gitops/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/IHaveASegway/gitops/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/IHaveASegway/gitops/compare/v0.1.0...v1.0.0
[0.1.0]: https://github.com/IHaveASegway/gitops/releases/tag/v0.1.0
