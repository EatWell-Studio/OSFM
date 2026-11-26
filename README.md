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

Public project documentation lives in [`docs/`](docs/README.md): the [architecture](docs/architecture.md) and the initial product research in [`docs/research/`](docs/research/README.md) (product form, technology stack, open-source landscape, map data and compliance).

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

`./data` holds `pb_data/` (database), `tiles/` (map tiles) and `secrets/` (instance keys, generated on first start; keep them in your backups). Settings come from environment variables: `OSFM_DATA_DIR` (default `./data`), `OSFM_LISTEN` (default `127.0.0.1:8090`; `serve --http` overrides it), `OSFM_PUBLIC_URL`, `OSFM_ADMIN_UI` (default `false`) and `OSFM_ADMIN_LISTEN` (default `127.0.0.1:8091`). The effective configuration is printed at startup.

Clients only use `/api/osfm/*`: PocketBase's own APIs and its admin UI are never served on the main listener (every other `/api/` path answers with a JSON 404). With `OSFM_ADMIN_UI=true`, the admin UI and PocketBase's APIs are served on a separate listener at `OSFM_ADMIN_LISTEN`, which your reverse proxy should not forward; reach it through an SSH tunnel (`ssh -L 8091:127.0.0.1:8091 your-server`, then open `http://127.0.0.1:8091/_/`). In a container, set `OSFM_ADMIN_LISTEN=0.0.0.0:8091` and publish the port on the host's loopback only (`-p 127.0.0.1:8091:8091`). Superusers are created with `osfm superuser upsert EMAIL PASS`.

For frontend work, run `./osfm serve` and `pnpm --dir web dev` side by side; the Vite dev server proxies `/api` and `/tiles` to the binary. Without a frontend build the binary still compiles and serves a placeholder page.

### Basemap tiles

The map's basemap is one PMTiles file per city in `<data>/tiles/` (`OSFM_DATA_DIR`, default `./data`), served as `/tiles/<city>/{z}/{x}/{y}.mvt` and `/tiles/<city>.json`. To get one for local development, install the [`pmtiles` CLI](https://github.com/protomaps/go-pmtiles/releases) and extract your city's bounding box from the latest [Protomaps](https://protomaps.com/) OpenStreetMap build; only the needed parts are downloaded, typically 10–30 MB per city:

```bash
deploy/tiles.sh --dry-run example-town 6.68,0.30,6.76,0.36   # estimate the size
deploy/tiles.sh example-town 6.68,0.30,6.76,0.36             # writes ./data/tiles/example-town.pmtiles
```

City names are lowercase letters, digits and `-`. Tile files are never committed.

Before opening a pull request, run `bin/gate.sh` (lint, type checks, tests and builds for both halves; see [CONTRIBUTING.md](CONTRIBUTING.md)).

## Container image

Each release publishes a multi-arch image (linux/amd64, linux/arm64) to the GitHub Container Registry, and the `osfm` binaries for Linux and macOS on the [releases page](../../releases). Nothing is published before the first release.

```bash
docker run -d --name osfm -p 127.0.0.1:8090:8090 -v osfm-data:/data ghcr.io/eatwell-studio/osfm:latest
```

The instance then answers on http://127.0.0.1:8090; put a reverse proxy with HTTPS in front of it to reach it from elsewhere. All data lives in the `/data` volume (`pb_data/`, later also `tiles/` and `secrets/`). The container runs as user 65532; a named volume as above gets the right owner automatically, a host directory mounted instead must be writable by that user. To build the image yourself: `docker buildx build -f deploy/Dockerfile .` (add `--build-arg VERSION=v1.2.3` to set the version that `osfm --version` reports; it is `dev` otherwise). Every image and binary carries a signed build provenance attestation:

```bash
gh attestation verify oci://ghcr.io/eatwell-studio/osfm:latest --repo EatWell-Studio/OSFM
gh attestation verify osfm-linux-amd64 --repo EatWell-Studio/OSFM
```

Self-hosting (HTTPS, backups, upgrades) will be documented separately.

## License

The software is licensed under the [GNU Affero General Public License v3.0 or later](LICENSE). If you run a modified version as a network service, you must offer its source code to its users; running it unmodified for your own circle carries no extra obligation.

The data an instance holds is licensed by layer:

- **Places** (positions, names, categories, position notes, OpenStreetMap references) are mixed with OpenStreetMap data and are therefore under the [Open Database License 1.0](https://opendatacommons.org/licenses/odbl/1-0/).
- **Recommendations** (what members write about a place) are under [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/).
- **Member data** is not licensed to anyone and never leaves the instance except in a member's own data export.

Map data © [OpenStreetMap contributors](https://www.openstreetmap.org/copyright), available under the Open Database License.
