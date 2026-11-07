# Security audit, 2026-11-07

Target: `vukem-tools` at branch `chore/scorecard-readme`.
Method: the vendored Cloudflare `security-audit` skill (`.claude/skills/security-audit`), full audit mode, profile `quick`.

Machine-readable records: [findings](security-audit-2026-11-07.findings.json), [coverage ledger](security-audit-2026-11-07.coverage.json).

## Scope and coverage statement

Partial coverage by design. Profile `quick`: one reviewer, coarse ledger units (5), one hunting pass, no follow-up waves. Source review only; no live endpoints were probed and no dependency was added. Scanners: `go tool govulncheck ./...` (0 reachable vulnerabilities) and `bun audit --prod` (none).

In scope: the yt-dlp downloader (URL validation, platform allowlist, SSRF), archive extraction (path, link and ratio limits), the CEP and CNPJ lookup client, API key middleware, Telegram bot transport and the GitHub workflows.

Not reviewed in depth: the image, video, audio and PDF engines, sprite and editor tools, quota accounting, device links and the vukem framework itself. The code base is large (about 49k lines) and was sampled, not read in full.

## Summary

| Severity | Confirmed | Needs validation | Rejected |
| --- | --- | --- | --- |
| Critical | 0 | - | - |
| High | 0 | - | - |
| Medium | 0 | - | - |
| Low | 0 | - | - |
| Informational | 1 | - | - |
| Unrated | - | 0 | 2 |

No confirmed code finding. `docs/vukem-audit.md` records earlier hardening that this pass found intact.

## Findings

### SA-001 First-party Actions pinned by tag (informational)

- Fingerprint: `vukem-tools:ci:actions-pinned-by-tag`
- CWE: CWE-829 (inclusion of functionality from an untrusted control sphere)
- Location: `.github/workflows/ci.yml:30`, `release.yml:19`
- Impact: A moved upstream tag would run attacker code with the job token; release workflows hold `packages: write`.
- Fix: Every `uses:` is pinned to a commit SHA with the tag in a comment. Status: fixed in `f46d4f2`.

## Rejected after validation

- `vukem-tools:downloader:ssrf`: Three independent guards.
- `vukem-tools:archive:zip-slip-and-bomb`: Guards present and documented.

Hardening notes, not findings: `security` is not an allowed commit scope here, so the fix commit uses the `ci` type without a scope.

## Verification

Findings and ledger passed the skill validators (`validate-findings.cjs`: 3 records; `validate-coverage-ledger.cjs`: 5 units), run on Windows Node with a preload shim for the missing POSIX no-follow constants. The skill's own CLI tests fail 7 cases on Windows for the same reason, as in `vukem-email`.
