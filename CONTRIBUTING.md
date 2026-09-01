# Contributing to gitops

Thanks for helping out. This document explains how the project is laid out,
how to run it locally and what a good pull request looks like.

## Getting started

You need Go 1.27+ and git. The [gh CLI](https://cli.github.com/) and
[golangci-lint](https://golangci-lint.run/) are optional but recommended.

```bash
git clone https://github.com/IHaveASegway/gitops.git
cd gitops
make build          # ./gitops with the version stamped in
make check          # vet + lint + tests — what CI runs
make help           # every target
```

`go run ./cmd/gitops` works too.

## Project layout

```
cmd/gitops/          main package — a two-line entry point
internal/
  cli/               urfave/cli wiring: subcommands, flags, the init command, prompts
  tui/               Bubble Tea UI, one file per view (menu, picker, input, confirm, exec, results)
  ops/               the git operations (pull, sync, reset, branch, checkout, push, status)
  clone/             `init`: plan building, duplicate-checkout detection, cloning
  github/            GitHub REST client, org-name parsing, token/protocol discovery
    githubtest/      fake GitHub API server for tests
  git/               running git, inspecting repos, discovering/scanning directories, remote URLs
  runner/            bounded-parallel executor with ordered results and progress events
  report/            colored CLI output
  format/            small text helpers (pluralization, path shortening)
  buildinfo/         version reporting
  testutil/          helpers that create real git repositories for tests
```

Dependencies only point downwards: `cli` and `tui` sit on top of `ops`,
`clone` and `github`; those sit on `git`, `runner`, `report` and `format`.
Nothing in `internal/` imports `cli` or `tui`.

A few conventions worth knowing:

- **Every operation is a `runner.Func`** (`func(ctx, target) runner.Result`).
  That is what lets the same code run headless from the CLI and with live
  progress in the TUI.
- **The TUI is a plain Bubble Tea model.** Views are switched with the
  `view` enum in `internal/tui/model.go`; each view owns its `updateX` and
  `renderX` pair. Background work (status loading, listing an org, running
  operations) streams messages through channels via `tea.Cmd`s.
- **Never prompt from git.** All git subprocesses run with
  `GIT_TERMINAL_PROMPT=0` (see `git.Env`) so a repository without access
  fails fast instead of hanging a parallel run.
- **Tokens never touch disk.** HTTPS clones authenticate through a
  process-scoped `GIT_CONFIG_*` override (`clone.env`), not the remote URL.

## Tests

```bash
make test           # or: go test ./...
make test-race
```

Tests create real repositories in temporary directories with
`internal/testutil` and talk to a fake GitHub API from
`internal/github/githubtest`; nothing needs network access or credentials.
The TUI is tested by driving the model directly (`internal/tui/harness_test.go`
feeds key presses and pumps commands until the model is quiescent), so a
flow such as *menu → picker → run → results* runs in a few hundred
milliseconds without a terminal.

When you add behavior, add a test at the same level: a pure function gets a
table test, a git operation gets a temp-repo test, a UI change gets a
model-driven test that asserts on `View()`.

## Code style

- `gofmt`/`goimports` formatting with the local import group last
  (`make fmt` applies both).
- `golangci-lint run` must be clean; the configuration is in `.golangci.yml`.
- Exported identifiers have doc comments. Package doc comments explain the
  package's job in one or two sentences.
- Errors are values shown to users: keep messages lowercase, specific, and
  say what to do next when there is an obvious next step.

## Commits and pull requests

- Keep commits focused; use [Conventional Commits](https://www.conventionalcommits.org/)
  prefixes (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `ci:`, `chore:`).
  They drive the release notes.
- Update `CHANGELOG.md` under *Unreleased* for user-visible changes.
- Fill in the pull request template. CI runs lint, `-race` tests on Linux,
  macOS and Windows, govulncheck, and cross-compiles every target.

## Releasing

1. Move the *Unreleased* section of `CHANGELOG.md` under the new version.
2. Tag and push: `git tag v1.2.0 && git push origin v1.2.0`.

One-time, after the first release that publishes a formula: delete the old
`Casks/gitops.rb` from the tap so `brew install IHaveASegway/tap/gitops` is
unambiguous (existing cask installs migrate with
`brew uninstall --cask gitops && brew install gitops`).

The `Release` workflow runs GoReleaser, which builds archives for
Linux/macOS/Windows on amd64/arm64 (reproducible: `-trimpath`, commit
timestamps), generates checksums and release notes, signs the checksums with cosign
(keyless, via the workflow's OIDC identity), stamps the version into
the binary (`gitops --version`), and pushes an updated formula to
[IHaveASegway/homebrew-tap](https://github.com/IHaveASegway/homebrew-tap) so
`brew install IHaveASegway/tap/gitops` (and `brew upgrade`) pick it up.

The tap push authenticates over SSH with the `HOMEBREW_TAP_DEPLOY_KEY`
repository secret: the private half of a write-enabled deploy key that is
registered only on `homebrew-tap`, so it cannot touch anything else. Without
it the release still succeeds and the formula is only written to `dist/`.

`make snapshot` produces the same archives (and the formula under
`dist/homebrew/`) locally without publishing anything.
