# OSFM — OpenStreetFoodMap

A food map for a circle of friends, on top of OpenStreetMap. **The map is only the display; the recommendations of people you know are the data.**

> 🚧 Early development — not usable yet. Design: [docs/architecture.md](docs/architecture.md); research: [docs/research](docs/research/README.md).

一个建立在 OpenStreetMap 之上、只属于一个熟人圈的美食地图：地图只是展示层，群友署名的推荐才是数据层。

## Idea

- **One circle, one instance.** Each group of friends runs its own instance; there is no central database that mixes circles.
- **Vouched, not crowdsourced.** Joining takes an invitation, and every recommendation carries the name (or chosen pseudonym) of the member who made it — the cost of spamming the map is becoming a member of the circle.
- **Open map, own data.** OpenStreetMap as the base map; the circle's recommendations stay on the circle's own server and can be exported at any time.
- **Small enough for a set-top box.** A single process with SQLite is the target for the first version.

## Documentation

Public project documentation lives in [`docs/`](docs/README.md): the [self-hosting guide](docs/self-hosting.md) (draft), the [architecture](docs/architecture.md) and the initial product research in [`docs/research/`](docs/research/README.md) (product form, technology stack, open-source landscape, map data and compliance).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. All commits must be signed off (DCO).

## Security

Please report vulnerabilities privately — see [SECURITY.md](SECURITY.md).

## Development

Requirements: Go (version in `go.mod`), Node.js 22+ and pnpm (version in `web/package.json`).

```bash
pnpm --dir web install
pnpm --dir web build          # builds the web app into web/dist/app
go build -o osfm ./cmd/osfm   # one binary with the web app embedded
./osfm serve                  # http://127.0.0.1:8090, data in ./data
```

### First start

On its first start, with an empty data directory, the instance has no maintainer yet. It prints a one-time **setup code** to its log:

```
WARN This instance has no maintainer yet. Open it in a browser and enter this setup code … setup_code=XXXX-XXXX-XXXX-XXXX
```

(`./osfm setup-code`, or `docker exec osfm osfm setup-code` for the container below, prints it again.) Open the instance in a browser: every page leads to the setup wizard. Enter the setup code, give the instance its name (it is saved together with the first maintainer and shown as the issuer in authenticator apps), create the first maintainer (password and authenticator app, or a passkey; two-step verification is required as for everyone, and you get recovery codes), then add at least one city. If you leave the wizard after creating the maintainer, signing in again brings you back to the cities. You end up signed in on the map; no configuration file needs editing. The setup code exists so that whoever happens to open a freshly deployed instance first cannot claim it: only someone who can read the server's log can. Each start while there is no maintainer makes a new code; once the first maintainer exists, the code and the wizard are gone. Further members join with invites from the maintainer.

What the wizard cannot do for you is place the basemap files: each city's basemap is a file in `tiles/` (see [Basemap tiles](#basemap-tiles)). The wizard offers the files already there and, for a city added without one, shows the `deploy/tiles.sh` command that extracts it; until the file is in place, the map shows that the city has no basemap yet.

`./data` holds `pb_data/` (database), `tiles/` (map tiles) and `secrets/` (instance keys, generated on first start; keep them in your backups). Settings come from environment variables: `OSFM_DATA_DIR` (default `./data`), `OSFM_LISTEN` (default `127.0.0.1:8090`; `serve --http` overrides it), `OSFM_PUBLIC_URL` (must be `https://`, except for `localhost` or a loopback address: the session cookie is `Secure`; set it in production: with an `https://` URL on a real host name, OSFM also sends HSTS and names the session cookie `__Host-osfm_session`), `OSFM_ADMIN_UI` (default `false`), `OSFM_ADMIN_LISTEN` (default `127.0.0.1:8091`) and `OSFM_TRUSTED_PROXY_HEADERS` (the header your reverse proxy puts the client's address in, usually `X-Forwarded-For`; the rightmost address is used). The effective configuration is printed at startup.

Behind a reverse proxy, set `OSFM_TRUSTED_PROXY_HEADERS`: the sign-in rate limits and lockouts count per client address, and without it every client shares the proxy's address. Then make sure only the proxy can reach OSFM's port (listen on `127.0.0.1`, or publish the container port on the host's loopback only), since anyone connecting directly could put any address in that header.

Without a session, only the sign-in pages, `/api/health` and `/.well-known/osfm.json` answer; the rest of `/api/osfm/*` and the tiles return 401. Clients only use `/api/osfm/*`: PocketBase's own APIs and its admin UI are never served on the main listener (every other `/api/` path answers with a JSON 404). With `OSFM_ADMIN_UI=true`, the admin UI and PocketBase's APIs are served on a separate listener at `OSFM_ADMIN_LISTEN`, which your reverse proxy should not forward; reach it through an SSH tunnel (`ssh -L 8091:127.0.0.1:8091 your-server`, then open `http://127.0.0.1:8091/_/`). In a container, set `OSFM_ADMIN_LISTEN=0.0.0.0:8091` and publish the port on the host's loopback only (`-p 127.0.0.1:8091:8091`). Superusers are created with `osfm superuser upsert EMAIL PASS`.

For frontend work, run `./osfm serve` and `pnpm --dir web dev` side by side; the Vite dev server proxies `/api` and `/tiles` to the binary. Without a frontend build the binary still compiles and serves a placeholder page.

### Basemap tiles

The map's basemap is one PMTiles file per city in `<data>/tiles/` (`OSFM_DATA_DIR`, default `./data`), served as `/tiles/<city>/{z}/{x}/{y}.mvt` and `/tiles/<city>.json`; `/tiles/` lists the cities. The map page offers the instance's cities (set in the setup wizard, by their display names) and any other city that has a file. To get one for local development, install the [`pmtiles` CLI](https://github.com/protomaps/go-pmtiles/releases) and extract your city's bounding box from the latest [Protomaps](https://protomaps.com/) OpenStreetMap build; only the needed parts are downloaded, typically 10–30 MB per city:

```bash
deploy/tiles.sh --dry-run example-town 6.68,0.30,6.76,0.36   # estimate the size
deploy/tiles.sh example-town 6.68,0.30,6.76,0.36             # writes ./data/tiles/example-town.pmtiles
```

City names are lowercase letters, digits and `-`, the same as a city's slug in the setup wizard; a file without a city in the instance settings is shown by its file name (`example-town` as "Example Town"). A file added or removed while the server runs shows up on the next page load. Tile files are never committed.

Before opening a pull request, run `bin/gate.sh` (lint, type checks, tests and builds for both halves, and `pnpm --dir web run check:urls`, which fails when the built web app contains a URL of another origin; `bin/gate.sh --e2e` adds the end-to-end tests; see [CONTRIBUTING.md](CONTRIBUTING.md)).

## Container image

Each release publishes a multi-arch image (linux/amd64, linux/arm64) to the GitHub Container Registry, and the `osfm` binaries for Linux and macOS on the [releases page](../../releases). Nothing is published before the first release.

```bash
docker run -d --name osfm -p 127.0.0.1:8090:8090 -v osfm-data:/data ghcr.io/eatwell-studio/osfm:latest
```

The instance then answers on http://127.0.0.1:8090 and leads to the setup wizard (see [First start](#first-start); the setup code is in `docker logs osfm`); put a reverse proxy with HTTPS in front of it to reach it from elsewhere, and set `OSFM_PUBLIC_URL` (`-e OSFM_PUBLIC_URL=https://food.example.org`). OSFM sends its own security headers (content security policy, `Referrer-Policy`, `Permissions-Policy` and others; see [docs/architecture.md](docs/architecture.md) §4.7), and `Strict-Transport-Security` for this host only when `OSFM_PUBLIC_URL` is `https://`: the proxy should pass them through unchanged and add no CORS headers. Add `includeSubDomains` or `preload` to HSTS at the proxy only if the whole domain is served over HTTPS. All data lives in the `/data` volume (`pb_data/`, `tiles/` and `secrets/`). The container runs as user 65532; a named volume as above gets the right owner automatically, a host directory mounted instead must be writable by that user. To build the image yourself: `docker buildx build -f deploy/Dockerfile .` (add `--build-arg VERSION=v1.2.3` to set the version that `osfm --version` reports; it is `dev` otherwise). Every image and binary carries a signed build provenance attestation:

```bash
gh attestation verify oci://ghcr.io/eatwell-studio/osfm:latest --repo EatWell-Studio/OSFM
gh attestation verify osfm-linux-amd64 --repo EatWell-Studio/OSFM
```

Running an instance for a circle (Docker Compose or systemd, Caddy for HTTPS, backups, upgrades, troubleshooting) is described in [docs/self-hosting.md](docs/self-hosting.md), a draft not yet tried on a real server; example configuration is in [`deploy/`](deploy/).

## License

The software is licensed under the [GNU Affero General Public License v3.0 or later](LICENSE). If you run a modified version as a network service, you must offer its source code to its users; running it unmodified for your own circle carries no extra obligation.

The data an instance holds is licensed by layer:

- **Places** (positions, names, categories, position notes, OpenStreetMap references) are mixed with OpenStreetMap data and are therefore under the [Open Database License 1.0](https://opendatacommons.org/licenses/odbl/1-0/).
- **Recommendations** (what members write about a place) are under [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/).
- **Member data** is not licensed to anyone and never leaves the instance except in a member's own data export.

Map data © [OpenStreetMap contributors](https://www.openstreetmap.org/copyright), available under the Open Database License.
