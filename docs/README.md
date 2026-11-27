# OSFM documentation

Public documentation of OSFM. Documents are written in Chinese for now, except the self-hosting guide, which is in English like the README; English summaries will follow with the first release.

| Directory | Content |
| --- | --- |
| [self-hosting.md](self-hosting.md) | Running an instance (draft): requirements, Docker Compose or systemd, Caddy and HTTPS, first start and basemaps, home connections with blocked ports, admin UI, backups, upgrades, logs, troubleshooting |
| [architecture.md](architecture.md) | Architecture: process and repository layout, backend (PocketBase usage, API, two-factor authentication, authorisation, tiles, search proxy, export), frontend stack, deployment, testing, decision records |
| [research/](research/README.md) | Initial product research (2026-11): product form, technology stack, open-source landscape, map data and compliance |

Data model details and the import/export format will be added here as the code is written.

## What is not here

Planning documents with undecided product decisions, internal discussions and anything containing members' identities are kept in a private repository and never committed here. Contributors do not need them: decisions that affect the code are written up here before they are implemented.
