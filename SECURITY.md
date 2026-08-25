# Security policy

## Reporting a vulnerability

Please do not open a public issue for security problems. Use GitHub's private
vulnerability reporting instead:

https://github.com/IHaveASegway/gitops/security/advisories/new

You will get an acknowledgement within a few days. Fixes are released as a
new version with a note in the changelog; credit is given unless you prefer
otherwise.

## Supported versions

Only the latest release receives fixes.

## How gitops handles credentials

- Tokens come from `GH_TOKEN`, `GITHUB_TOKEN` or the gh CLI and are held in
  memory only.
- HTTPS clones authenticate through a process-scoped git configuration
  override (`GIT_CONFIG_*` environment variables). The token is never written
  to `.git/config`, never embedded in a remote URL, and does not appear in
  the process list.
- Remote URLs are read from `.git/config` for duplicate detection and are
  never printed with their userinfo.
- git runs with `GIT_TERMINAL_PROMPT=0`, so it never asks for credentials
  interactively.
