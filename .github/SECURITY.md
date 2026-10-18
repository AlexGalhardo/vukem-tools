# Security policy

Vukem Tools accepts arbitrary files from the internet and hands them to decoders and media tools
written in C. That is the whole attack surface, and it is treated as such: the threat model, the
engine CVE history and the mitigations are in [docs/web-research.md](../docs/web-research.md), and
the checklist every new route and engine call follows is in
[`.claude/skills/vukem-tools-secure-endpoint`](../.claude/skills/vukem-tools-secure-endpoint/SKILL.md).

## Supported versions

| Version | Supported |
| --- | --- |
| Latest `0.x` minor | ✅ |
| Older `0.x` minors | ❌ |

Before `1.0.0` only the latest minor is supported. Upgrade before reporting.

## Reporting a vulnerability

**Do not open a public issue.**

Report privately through GitHub: **Security → Report a vulnerability** on
<https://github.com/AlexGalhardo/vukem-tools/security/advisories/new>.

Please include:

- The version or commit
- What an attacker achieves, not only what misbehaves
- A reproduction: the smallest input file or request that triggers it
- Whether it needs an authenticated account, and which abilities its API key carries

A first response should arrive within a few days. This is a personal open-source project with no
bug bounty, no service-level agreement and no paid support.

## What is in scope

- Escaping a conversion job: reading or writing outside its working directory, reaching the
  network from a subprocess that should have none, or surviving the job's timeout
- Reaching another user's files, conversions or API keys
- Bypassing authentication, email verification, an API key's abilities, the rate limit or the quota
- Server-side request forgery through a URL input, including the link-local metadata endpoints
- Denial of service out of proportion to the input: a decompression bomb, a zip bomb, a pathological
  glob or an input that pins a CPU past its limit
- Anything that makes the app serve a file as a type it is not, or hand out a signed URL for an
  object the requester may not read

## What is out of scope

- Vulnerabilities in the conversion engines themselves (ffmpeg, libvips, Ghostscript, yt-dlp and
  the rest). Report those upstream; tell us too, so the pinned version can be moved.
- Reports from a deployment that disabled the sandboxing, ran the container as root, or exposed the
  app without authentication
- Missing hardening with no demonstrated impact, and automated scanner output with no reproduction
- Advisories with no fixed release in a dev-only dependency, which are documented in the workflow
  that ignores them (see [`.claude/rules/ci.md`](../.claude/rules/ci.md))
- Denial of service that needs a valid account and stays inside the published limits — that is the
  quota working

## Operating it safely

If you self-host this, the deployment is yours to secure:

- Keep the engines current. They are pinned in the `Dockerfile` precisely so a bump is a reviewed,
  visible change, not so it can be skipped.
- Leave authentication on. There is no anonymous mode by design; adding one is adding a
  denial-of-service surface and an abuse surface at once.
- Run the container as the non-root user it ships with, never `--privileged`.
- Keep the result TTL short. Results are meant to expire, and a full disk takes the service down.
- Do not point it at a database or a bucket that holds anything else.
