# Contributing to OSFM

Thanks for your interest in OSFM! This document explains how to report problems, propose changes and submit work.

## Where to go

| You want to… | Please use |
| --- | --- |
| Report a bug | [GitHub issues](../../issues/new/choose) → "Bug report" |
| Suggest a feature | [GitHub issues](../../issues/new/choose) → "Feature request" |
| Report a security vulnerability | **Privately**, see [SECURITY.md](SECURITY.md) |

## Ground rules

- Be respectful and constructive.
- The trust model is the product: changes that touch registration, invitations, visibility of recommendations or anything that could expose a member's identity get extra scrutiny and must come with tests.
- Never commit real instance data — member names, recommendations, database files, uploads — not even as test fixtures. Use made-up data.
- Map data: follow the [OpenStreetMap licence and attribution rules](https://www.openstreetmap.org/copyright). Never copy data from commercial map providers (Google, Amap/Gaode, Baidu, Dianping, …) into this repository or into OpenStreetMap.
- Discuss larger changes in an issue before writing code, so nobody wastes effort.

## What does not belong in this repository

This is a public repository. The following are kept out by `.gitignore` **and** rejected by a check that runs in CI and in the pre-commit hook (`bin/check-private-paths.sh`), so `git add -f` will not get them in either:

- Configuration and instructions of AI coding assistants (`CLAUDE.md`, `.claude/`, `.mcp.json`, `AGENTS.md`, `.cursor/`, …)
- Internal documents (`docs/internal`)
- Secrets (`.env`, keys, credentials) and instance data (`*.sqlite`, `*.db`, `data/`, `uploads/`, backups)
- Map tile extracts (`*.pmtiles`, `*.mbtiles`, `*.osm.pbf`) and any symbolic link

Install the hook once after cloning:

```bash
bin/install-hooks.sh
```

## Branches, commits and pull requests

- `main` is protected: no direct pushes, no force pushes; every change arrives through a pull request and is squash-merged.
- Branch names: `t0xx-short-description` for planned tasks, `fix-…`, `docs-…` or `chore-…` otherwise.
- Use short, imperative commit subjects and reference the task or issue when there is one:

```
T003: store recommendations with a stable UUID
Fix #12: keep pseudonyms out of the export
```

- The pull request description explains *why*, not only *what*.

## Developer Certificate of Origin (sign-off required)

To make sure every contribution can be distributed under the project's license, all commits must be signed off. By adding a `Signed-off-by` line you certify the [Developer Certificate of Origin 1.1](https://developercertificate.org/): that you wrote the contribution or otherwise have the right to submit it under the project's license.

```bash
git commit -s -m "Your message"
```

Pull requests with unsigned commits cannot be merged.
