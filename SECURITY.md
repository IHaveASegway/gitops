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

- Tokens come from `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`,
  `GITHUB_ENTERPRISE_TOKEN` or the gh CLI and are held in memory only.
- Tokens are scoped to the host they belong to: `GH_TOKEN`/`GITHUB_TOKEN`
  are sent to github.com only, and `GH_ENTERPRISE_TOKEN`/
  `GITHUB_ENTERPRISE_TOKEN` only to the enterprise host you name. A
  github.com token is never sent to any other host, including mistyped or
  look-alike host names.
- API pagination links are followed only on the same scheme and host the
  listing started on, and repository clone URLs reported by the API are
  validated against the resolved host/owner/name — a compromised or spoofed
  API endpoint cannot redirect requests or clones elsewhere.
- `GITOPS_GITHUB_API` (the API base override used by tests and trusted
  proxies) must be an `https://` URL; `http://` is accepted for localhost
  only. Anything else is ignored with a warning.
- HTTPS clones authenticate through a process-scoped git configuration
  override (`GIT_CONFIG_*` environment variables). The token is never
  written to `.git/config`, never embedded in a remote URL, and never
  passed on a command line. It is present in the environment of gitops'
  own short-lived git subprocesses (the same mechanism GitHub Actions
  uses), which is readable only by processes of the same user — and any
  same-user process could also read it from `$GH_TOKEN` or the gh keychain
  entry it came from.
- Remote URLs are read from `.git/config` for duplicate detection and are
  never printed with their userinfo.
- git runs with `GIT_TERMINAL_PROMPT=0`, so it never asks for credentials
  interactively.

## Release integrity

Release checksums (`checksums.txt`) are signed with [cosign](https://docs.sigstore.dev/)
using the release workflow's keyless OIDC identity. To verify a download:

```bash
cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp 'https://github.com/IHaveASegway/gitops/\.github/workflows/release\.yml@refs/tags/v.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing
```
