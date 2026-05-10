# Security policy

## Supported versions

OpenUsage follows semantic versioning. Security fixes land on the latest minor release line. Patch releases are cut as needed and published to the [GitHub releases page](https://github.com/janekbaraniewski/openusage/releases).

| Version line | Supported |
|---|---|
| 0.10.x | ✅ active |
| < 0.10 | ❌ end of life |

We aim to keep CVE windows short. If a high-severity issue is reported against an in-support release line, expect a patch within a few days.

## Reporting a vulnerability

**Please do not file public GitHub issues for security problems.**

Use [GitHub's private vulnerability reporting](https://github.com/janekbaraniewski/openusage/security/advisories/new) instead. It opens a private advisory channel between you and the maintainers.

If you can't use that channel, email `security@baraniewski.com` with:

- A clear description of the issue and its impact
- Steps to reproduce, or a proof-of-concept
- The version of OpenUsage where you observed it (`openusage version`)
- The platform and Go version (`go version`)
- Any suggested mitigation, if you have one

You'll get an acknowledgement within **3 business days**, an initial assessment within **7 business days**, and updates at least weekly until the issue is resolved or marked out of scope.

## Disclosure

We follow a coordinated-disclosure model:

1. The reporter and maintainers privately scope the issue and produce a fix.
2. A patched release is published.
3. A GitHub Security Advisory is published with a CVE (if applicable) and credit to the reporter.
4. After 30 days the original report is made public, unless extended by mutual agreement.

Researchers acting in good faith are welcome and credited in the advisory unless they prefer otherwise.

## Scope

In scope:

- The `openusage` binary, including the dashboard TUI, the daemon, and the integrations command
- Provider auth flows and any code that handles credentials, cookies, or session data
- The telemetry pipeline, SQLite store, and Unix-socket protocol
- The published Homebrew tap and release artifacts

Out of scope:

- Issues that require local access to a logged-in user's machine to exploit
- Reports against third-party providers' APIs (those go to the vendor)
- Theoretical issues with no demonstrated impact

## Hardening

This project participates in:

- [GitHub Dependabot](https://github.com/dependabot) for dependency updates and security advisories
- [GitHub CodeQL](https://codeql.github.com/) for static analysis
- [`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for Go-specific vulnerability scanning
- [OpenSSF Scorecard](https://scorecard.dev/) for supply-chain hygiene
- [Sigstore cosign](https://www.sigstore.dev/) keyless signing of release binaries (GitHub OIDC identity)

Release checksums are published alongside binaries on the [releases page](https://github.com/janekbaraniewski/openusage/releases).
