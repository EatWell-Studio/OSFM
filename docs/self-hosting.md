# Self-hosting OSFM

How to run an OSFM instance for your circle: on a small server, behind Caddy or nginx for HTTPS, with backups and upgrades. The design behind it is in [architecture.md](architecture.md) (§5 covers deployment and operations).

> **Draft.** This guide is written from the code and its tests. Nothing in it has yet been tried end to end on a real server, and no release has been published yet. Steps that have not yet been verified on a real deployment are marked _(not yet verified)_. The first real deployment will check them and this guide will be updated.

Contents: [Requirements](#requirements) · [Run OSFM](#run-osfm) · [Caddy and HTTPS](#caddy-and-https) · [nginx](#nginx-and-control-panels) · [Cloudflare](#cloudflare) · [First start](#first-start) · [Home connection with blocked ports](#home-connection-with-ports-80-and-443-blocked) · [Where not to host](#where-not-to-host-mainland-china) · [Admin UI](#admin-ui) · [Backups](#backups) · [Upgrades](#upgrades) · [Logs](#logs) · [Troubleshooting](#troubleshooting)

Example files: [`deploy/compose.example.yml`](../deploy/compose.example.yml), [`deploy/osfm.service.example`](../deploy/osfm.service.example), [`deploy/Caddyfile.example`](../deploy/Caddyfile.example), [`deploy/nginx.conf.example`](../deploy/nginx.conf.example). Throughout, `food.example.org` stands for your instance's host name and `example-city` for a city.

## Requirements

- **A server**: a small Linux VPS (amd64 or arm64), or a small machine at home (see [below](#home-connection-with-ports-80-and-443-blocked)). OSFM is one process with an SQLite database.
  - Memory: the target is at most 300 MB resident when idle; this has not been measured yet. Password hashing takes up to 128 MiB while sign-ins are in progress (at most two at a time).
  - Disk: the program itself is well under 100 MB. Each city's basemap is typically 10–30 MB. The database of a circle is small, but its size in use has not been measured yet. Leave room for backups (each pre-migration backup is a copy of the database, see [Upgrades](#upgrades)).
- **A host name** (such as `food.example.org`) whose DNS `A`/`AAAA` records point at the server. It must be a name, not an IP address: passkeys are bound to it. Pick it before members register; see [Troubleshooting](#passkeys-fail).
- **Docker** with the Compose plugin, or the `osfm` binary with systemd.
- **A reverse proxy** for HTTPS and compression: Caddy 2 (the simplest, used in the examples below) or [nginx](#nginx-and-control-panels), for example on a server that already runs nginx or a control panel.
- To extract basemaps, on any machine: `curl` and the [`pmtiles` CLI](https://github.com/protomaps/go-pmtiles/releases) (the server does not need them).

## Run OSFM

OSFM is configured with environment variables; the effective configuration is logged at every start (`OSFM configuration …`).

| Variable                      | Default                                      | In production                                                                                                                                                                                                                                                                                                                                               |
| ----------------------------- | -------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `OSFM_PUBLIC_URL`             | not set                                      | `https://food.example.org`, the URL members open (with the port if it is not 443). Must be `https://` unless the host is `localhost` or a loopback address. With an `https://` URL on a real host name, OSFM sends HSTS and names the session cookie `__Host-osfm_session`. Passkeys are bound to its host name and accepted only from exactly this origin. |
| `OSFM_TRUSTED_PROXY_HEADERS`  | not set                                      | `X-Forwarded-For`: the header the proxy puts the client's address in; OSFM takes the rightmost address. Needed for the sign-in rate limits, see [Caddy](#caddy-and-https).                                                                                                                                                                                  |
| `OSFM_LISTEN`                 | `127.0.0.1:8090` (image: `0.0.0.0:8090`)     | Keep it reachable from the proxy only. `serve --http` overrides it.                                                                                                                                                                                                                                                                                         |
| `OSFM_DATA_DIR`               | `./data` (image: `/data`)                    | Where all data lives, see [Backups](#backups).                                                                                                                                                                                                                                                                                                              |
| `OSFM_ADMIN_UI`               | `false`                                      | Turn on only when needed, see [Admin UI](#admin-ui).                                                                                                                                                                                                                                                                                                        |
| `OSFM_ADMIN_LISTEN`           | `127.0.0.1:8091`                             | Only used with `OSFM_ADMIN_UI=true`.                                                                                                                                                                                                                                                                                                                        |
| `OSFM_GEOCODER_PHOTON_URL`    | `https://photon.komoot.io/api`               | Place search asks this Photon instance first. Leave it, or point it at a Photon of your own. Maintainers can turn place search off on the Admin page; search terms then never leave the server.                                                                                                                                                             |
| `OSFM_GEOCODER_NOMINATIM_URL` | `https://nominatim.openstreetmap.org/search` | Asked when Photon fails, at most once per second for the whole instance. Set a contact on the Admin page: the public services ask to be able to reach you.                                                                                                                                                                                                  |

The data directory holds `pb_data/` (the database), `tiles/` (one basemap file per city) and `secrets/` (the instance keys, generated on the first start). OSFM creates them itself.

### With Docker Compose

Each release publishes a multi-arch image to `ghcr.io/eatwell-studio/osfm`, tagged with the version (`1.2.3`), `1.2`, `1` (from 1.0.0 on) and `latest`; pre-releases only get their own version tag. **No release has been published yet**: until then, build the image yourself from a checkout of the repository with `docker buildx build -f deploy/Dockerfile -t osfm:local --load .` and use `osfm:local` as the image.

Copy [`deploy/compose.example.yml`](../deploy/compose.example.yml) to `compose.yml` in a directory of its own on the server, set the image tag and `OSFM_PUBLIC_URL`, then:

```bash
docker compose up -d
docker compose logs osfm      # the configuration and, on the first start, the setup code
```

What the example does:

- The data lives in the named volume `osfm-data`, mounted at `/data`. The container runs as user 65532; a new named volume gets the right owner automatically. A host directory mounted instead must be writable by that user.
- The port is published on the host's loopback interface only (`127.0.0.1:8090:8090`), so only the reverse proxy on the same host reaches it. Do not publish it on all interfaces: anyone who can connect directly can put any address in `X-Forwarded-For`.
- `OSFM_TRUSTED_PROXY_HEADERS=X-Forwarded-For`, `OSFM_PUBLIC_URL`, `restart: unless-stopped`, and log rotation for the container's output.
- The image has no shell or `curl`, so the example has no container health check; `https://food.example.org/api/health` answers 200 for external monitoring.

The image's own documentation is in the README's [Container image](../README.md#container-image) section, including how to verify its build provenance with `gh attestation verify`.

### With the binary and systemd

Each release also attaches `osfm-linux-amd64`, `osfm-linux-arm64` (and macOS builds) with a `SHA256SUMS` file to the [GitHub release](https://github.com/EatWell-Studio/OSFM/releases). Until the first release, build the binary as described in the README's [Development](../README.md#development) section. The example unit is [`deploy/osfm.service.example`](../deploy/osfm.service.example) _(not yet verified on a server)_:

```bash
sha256sum --check --ignore-missing SHA256SUMS
sudo install -m 0755 osfm-linux-amd64 /usr/local/bin/osfm
sudo useradd --system --home-dir /var/lib/osfm --shell /usr/sbin/nologin osfm
sudo cp osfm.service.example /etc/systemd/system/osfm.service   # then set OSFM_PUBLIC_URL in it
sudo systemctl daemon-reload
sudo systemctl enable --now osfm
journalctl -u osfm            # the configuration and, on the first start, the setup code
```

The unit runs OSFM as the dedicated user `osfm` with `OSFM_DATA_DIR=/var/lib/osfm` (created by systemd through `StateDirectory=`), listening on `127.0.0.1:8090`, with some sandboxing options.

Run the other `osfm` commands as the same user and with the same `OSFM_DATA_DIR`, never as root (root would leave files in the data directory that the service cannot open):

```bash
sudo -u osfm env OSFM_DATA_DIR=/var/lib/osfm /usr/local/bin/osfm setup-code
```

## Caddy and HTTPS

Install Caddy 2 on the same host (for example from its Debian/Ubuntu package, which runs it as a service with `/etc/caddy/Caddyfile`), and start from [`deploy/Caddyfile.example`](../deploy/Caddyfile.example) _(not yet verified on a server; the example has not been checked with `caddy validate` either)_:

```caddyfile
food.example.org {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8090
}
```

With a host name as site address, Caddy obtains and renews the certificate itself and redirects HTTP to HTTPS; ports 80 and 443 must be reachable from the internet. Then `systemctl reload caddy`.

What matters for OSFM:

- **Compression**: OSFM sends its responses uncompressed, including the map's JavaScript. `encode` compresses them at the proxy.
- **Security headers**: OSFM sends its own (`Content-Security-Policy`, `Referrer-Policy`, `Permissions-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, the cross-origin policies, and HSTS for this host; see [architecture.md](architecture.md) §4.7). Caddy's `reverse_proxy` passes response headers through unchanged. Do not add `header` directives that replace or remove them or `Set-Cookie`, and do not add CORS headers.
- **HSTS**: with `OSFM_PUBLIC_URL=https://…` on a real host name, OSFM sends `Strict-Transport-Security` with a one-year `max-age`, for this host only. If every subdomain of your domain is served over HTTPS and you want `includeSubDomains` (or `preload`, which also requires `includeSubDomains`), replace the header at the proxy, inside `reverse_proxy`, so that there is still only one:

  ```caddyfile
  reverse_proxy 127.0.0.1:8090 {
  	header_down Strict-Transport-Security "max-age=31536000; includeSubDomains"
  }
  ```

  Both are hard to undo: browsers remember them for the whole `max-age`, and preload lists take months to update.

- **Client address**: `reverse_proxy` sets `X-Forwarded-For`; the rightmost address in it is always the one Caddy received the request from. Set `OSFM_TRUSTED_PROXY_HEADERS=X-Forwarded-For` so that OSFM counts the sign-in rate limits and lockouts per client. Without it, every client counts as the proxy's address and one person's failed attempts lock out everyone (see [Troubleshooting](#everyone-gets-too-many-attempts)).
- **Reachable only through the proxy**: OSFM's port must not be reachable from anywhere but the proxy (`OSFM_LISTEN=127.0.0.1:8090`, or the container port published on `127.0.0.1` only). Whoever can connect directly can write any address into `X-Forwarded-For` and get around the per-client limits (the per-account limits still apply). Other containers on the same Docker network can reach the container port too; do not put untrusted ones there.
- **`OSFM_PUBLIC_URL`**: set it to exactly the URL members open, `https://food.example.org`. It decides the passkey origin, the `__Host-` cookie name and HSTS.
- **The admin listener** (`OSFM_ADMIN_LISTEN`) is never proxied; see [Admin UI](#admin-ui).

## nginx and control panels

On a server that already runs nginx, for example under a control panel such as aaPanel, use nginx instead of Caddy. Start from [`deploy/nginx.conf.example`](../deploy/nginx.conf.example) _(not yet verified on a server; checked locally with nginx 1.31 in front of OSFM: `nginx -t`, security headers and caching passed through, scripts compressed, 404 for missing files, a client's own `X-Forwarded-For` replaced)_; the core of it:

```nginx
location / {
    proxy_pass http://127.0.0.1:8090;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

The rules from [Caddy and HTTPS](#caddy-and-https) apply in the same way; with nginx:

- **Compression**: nginx compresses nothing from a proxy by default. Turn on `gzip` with `gzip_proxied any` and the types in the example. Basemap tiles already arrive compressed, with their own `Content-Encoding`, and nginx leaves them alone.
- **Client address**: set `X-Forwarded-For` to `$remote_addr`, a single address, and `OSFM_TRUSTED_PROXY_HEADERS=X-Forwarded-For`. (`$proxy_add_x_forwarded_for` also works, since OSFM takes the rightmost address, which is still `$remote_addr`.)
- **Security headers and cookie**: nginx passes OSFM's response headers and `Set-Cookie` through unchanged. Add no `add_header`, `proxy_hide_header` or `proxy_cookie_*` directives for them and no CORS headers. For HSTS with `includeSubDomains`, hide OSFM's header and add your own, as commented in the example, so that there is only one.
- **Everything goes to OSFM**: the site needs a single `location /`. Location blocks that serve `*.js`, `*.css` or images from a local directory, or set their caching, break the app (its scripts then come back 404 or with the wrong caching).
- **Certificate**: any ACME client (certbot, or the panel's own Let's Encrypt support); renewals must reload nginx.

**Control panels** (aaPanel, BaoTa and similar) generate the nginx site configuration and keep their own certificate paths. Create the site as a plain static site without PHP or a database, request the certificate there, then check the generated configuration _(not yet verified with a panel)_:

- Remove the default location blocks for static files (typically matching `gif|jpg|jpeg|png|bmp|swf|js|css` with `expires`), or OSFM's scripts and icons are looked for in the site's empty directory.
- Prefer writing the `location /` block above into the site's configuration over the panel's "reverse proxy" feature, or check what that feature generates: it must not cache responses, replace content or add headers, and it must set `X-Forwarded-For` as above.
- Leave the panel's HSTS switch off: OSFM sends HSTS itself, and the panel's would be a second header. Its "force HTTPS" switch is fine.
- Panels often have a firewall of their own. Docker's published ports bypass host firewalls, which is one more reason to publish OSFM's port on `127.0.0.1` only. In the cloud provider's firewall (for example the instance firewall of a VPS), open only 80 and 443 (and the SSH port), never 8090 or 8091.
- Check that the server has room: the panel and its services (a database, PHP) already use memory, and OSFM needs up to about 128 MiB more while sign-ins are in progress.

## Cloudflare

If the domain's DNS is at Cloudflare, the instance's record can be **DNS only** (grey cloud) or **proxied** (orange cloud). **Use DNS only** for OSFM's host name:

- Proxied, Cloudflare terminates TLS and sees every request and response, including session cookies and members' recommendations. OSFM sends no data to third parties by default; this would make Cloudflare one.
- Several Cloudflare features inject scripts or rewrite pages, which OSFM's Content-Security-Policy blocks or which break the API (see below).
- With DNS only, the server sees the clients' addresses directly, and the setup above works unchanged.

DNS only still lets you use Cloudflare's API for the certificate's DNS challenge (as in the [home-connection](#home-connection-with-ports-80-and-443-blocked) section).

If you proxy anyway _(not yet verified)_:

- **SSL/TLS mode "Full (strict)"**, with a publicly trusted certificate or a Cloudflare origin certificate on the server. Never "Flexible": the request would reach the server unencrypted.
- **Client address**: requests come from Cloudflare's addresses. Let nginx take the client's address from `CF-Connecting-IP`, but only from Cloudflare's address ranges (`set_real_ip_from` for each range at [cloudflare.com/ips](https://www.cloudflare.com/ips/), `real_ip_header CF-Connecting-IP`; commented in the example), keep the `X-Forwarded-For $remote_addr` line, and allow ports 80/443 at the cloud firewall from Cloudflare's ranges only. Without this, all members share the per-client limits of a few Cloudflare addresses, or anyone reaching the server directly can choose their address.
- **Turn off** for this host: Rocket Loader, Email Address Obfuscation, Web Analytics / Real User Monitoring (its automatic script injection), and challenge pages for the app's own requests (Bot Fight Mode, "Under Attack" mode, managed challenges on `/api/*`). They inject scripts that the Content-Security-Policy blocks, send members' browsing to Cloudflare, or answer the app's API requests with an HTML challenge.
- **No "Cache Everything" rules**: OSFM sets its own caching headers (API responses `no-store`, the app's hashed files immutable, tiles `private`), and Cloudflare's default respects them.
- Passkeys and the `__Host-` cookie work through the proxy, since the browser still sees `https://food.example.org`.

## First start

On its first start with an empty data directory, OSFM generates the instance keys in `secrets/` and, because the instance has no maintainer yet, prints a one-time **setup code** to its log:

```
WARN This instance has no maintainer yet. Open it in a browser and enter this setup code … setup_code=XXXX-XXXX-XXXX-XXXX
```

The code keeps whoever happens to open a freshly deployed instance first from claiming it: only someone who can read the server's log can. Each start while there is no maintainer makes a new code. To print the current one again:

```bash
docker compose exec osfm osfm setup-code                                   # Docker Compose
sudo -u osfm env OSFM_DATA_DIR=/var/lib/osfm /usr/local/bin/osfm setup-code   # systemd
```

1. Open `https://food.example.org`. Every page leads to the setup wizard.
2. Enter the setup code, then the instance's name (shown in the app and as the issuer in authenticator apps).
3. Create the first maintainer: a password with an authenticator app, or a passkey. Two-step verification is required, as for everyone. Keep the recovery codes.
4. Add at least one city: a display name, a slug (lowercase letters, digits and `-`, which is also the basemap's file name) and its bounding box. For a city without a basemap file, the wizard shows the `deploy/tiles.sh` command that extracts it.

If you leave the wizard after step 3, signing in again brings you back to the cities. Once the first maintainer exists, the setup code and the wizard are gone; further members join with invites.

### Basemaps

Each city's basemap is one file, `tiles/<slug>.pmtiles`, extracted from the latest [Protomaps](https://protomaps.com/) OpenStreetMap build with [`deploy/tiles.sh`](../deploy/tiles.sh) (zoom levels 10–15). Run it on any machine with `curl` and the `pmtiles` CLI, from a checkout of the repository; it downloads only the parts it needs. The bounding box is `minLon,minLat,maxLon,maxLat` in degrees (the example is a made-up city on the equator):

```bash
deploy/tiles.sh --dry-run example-city 6.68,0.30,6.76,0.36   # estimate the size only
deploy/tiles.sh example-city 6.68,0.30,6.76,0.36 ./tiles     # writes ./tiles/example-city.pmtiles
```

Then put the file into the instance's `tiles/` directory, which OSFM creates on its first start _(not yet verified on a server)_:

```bash
# Docker Compose: the file must be readable by the container's user (mode 0644 is enough)
docker compose cp ./tiles/example-city.pmtiles osfm:/data/tiles/example-city.pmtiles
# systemd
sudo install -o osfm -g osfm -m 0640 ./tiles/example-city.pmtiles /var/lib/osfm/tiles/
```

You can do this before or after the wizard; no restart is needed. A new or replaced file shows up on the next page load (the wizard's city step has a button to check again).

### Check the result

- `curl -sS https://food.example.org/api/health` answers 200.
- `curl -sS -o /dev/null -D - -H 'Accept-Encoding: gzip' https://food.example.org/` shows OSFM's security headers (`content-security-policy`, `strict-transport-security`, …) and `content-encoding` from the proxy _(not yet verified)_.
- The startup log line `OSFM configuration` shows `public_url=https://food.example.org`, `passkey_rp_id=food.example.org` and `trusted_proxy_headers=X-Forwarded-For`.
- In the browser, after signing in: the map shows each city, and the developer tools list the cookie `__Host-osfm_session` for the site _(not yet verified over HTTPS in a real browser)_.
- Sign in once with each method you expect members to use, from the devices they use.

## Home connection with ports 80 and 443 blocked

Some ISPs block incoming connections on ports 80 and 443 for home connections. Caddy's default certificate challenges need one of them, so use the **DNS-01 challenge** (Caddy proves control of the domain by creating a DNS record through your DNS provider's API) and serve on a **high port** _(not yet verified)_.

1. **A Caddy build with your DNS provider's module.** The standard Caddy has none. Options: build one with [`xcaddy`](https://github.com/caddyserver/xcaddy) (`xcaddy build --with github.com/caddy-dns/cloudflare`, for Cloudflare; the modules for other providers are listed at [caddy-dns](https://github.com/caddy-dns)), download a build with the module from [caddyserver.com/download](https://caddyserver.com/download), or build a container image from the official `caddy:<version>-builder` image. Check that `caddy list-modules` shows `dns.providers.<provider>`.
2. **An API token** for the DNS zone with permission to edit its records, given to Caddy as an environment variable (for the Debian package, with `systemctl edit caddy`: `Environment=CF_API_TOKEN=…`).
3. **The site on a high port**, here 8443, with the DNS challenge (the commented block in [`deploy/Caddyfile.example`](../deploy/Caddyfile.example)):

   ```caddyfile
   food.example.org:8443 {
   	tls {
   		dns cloudflare {env.CF_API_TOKEN}
   	}
   	encode zstd gzip
   	reverse_proxy 127.0.0.1:8090
   }
   ```

4. **Forward the port** on your router to the machine (TCP 8443; also UDP 8443 for HTTP/3).
5. **`OSFM_PUBLIC_URL=https://food.example.org:8443`**: the port is part of the origin that passkeys and the cookie are bound to, and members always open the URL with the port.

**Dynamic IP addresses**: Caddy does not update your DNS records. If your connection's public address changes, keep the `A`/`AAAA` records current with your router's dynamic DNS client or a script against your DNS provider's API, and give the records a short TTL: after a change, clients keep using the old address until their cached record expires. The certificate is not affected, since the DNS challenge does not depend on the address. Behind carrier-grade NAT (no public IPv4 address of your own) port forwarding does not work at all; ask your ISP, or use IPv6 if your members' networks have it.

## Where not to host: mainland China

**Public deployment in mainland China is not supported.** Offering a map service with user-contributed markers to the public there requires a surveying qualification and a map review approval that an individual cannot obtain (an OpenStreetMap-based map would not pass the review), on top of ICP filing. A private instance hosted in mainland China, reachable only from a LAN or a VPN and not as a public website, is at the deployer's own risk: whether it counts as an internet map service is unclear. The home-connection setup above is a technical description and does not change this. Details and sources: [research/geodata-and-compliance.md](research/geodata-and-compliance.md), §3. A server outside mainland China, with invite-only access, is the intended setup.

Other countries have their own rules; in Germany, for example, a site that is not purely private may need a legal notice (Impressum). The same research document has notes on this (§5); check what applies to you.

## Admin UI

PocketBase's admin UI reads all raw data, including which pseudonym belongs to whom. It is **off by default** and never served on the main port. Turn it on only when you need it, and off again afterwards.

With `OSFM_ADMIN_UI=true`, the admin UI and PocketBase's own APIs are served on a separate listener, `OSFM_ADMIN_LISTEN` (default `127.0.0.1:8091`). The reverse proxy must not forward it; reach it through an SSH tunnel:

```bash
ssh -L 8091:127.0.0.1:8091 you@food.example.org
# then open http://127.0.0.1:8091/_/ on your own machine
```

- **Docker**: the container's `127.0.0.1` is its own, so set `OSFM_ADMIN_LISTEN=0.0.0.0:8091` and publish the port on the host's loopback only (`127.0.0.1:8091:8091`; commented out in the compose example). OSFM logs a warning when the admin listener is not on a loopback address; here that is expected.
- **Superuser**: create one with `osfm superuser upsert EMAIL PASS` (`docker compose exec osfm osfm superuser upsert …`, or as the `osfm` user for systemd). This works whether the admin UI is on or not.
- After changing the variables, recreate the container (`docker compose up -d`) or restart the service.

## Backups

### What to back up

Everything that matters is in the data directory:

| Path                       | Content                                                                                                                                                                   | If it is lost                                                                                                                                                                                                                                                                                                                                                      |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `pb_data/`                 | The SQLite databases: `data.db` (members, credentials, invites, settings, audit log, …) and `auxiliary.db` (request logs only); `backups/` with the pre-migration backups | Everything the circle has entered                                                                                                                                                                                                                                                                                                                                  |
| `secrets/master.key`       | 32-byte key (AES-256-GCM). Members' TOTP secrets are stored encrypted with it; pending password registrations and sign-in tickets are sealed with it                      | The database alone is useless for TOTP: no member can sign in with their authenticator app any more. Passkeys and recovery codes do not depend on the key (they are public keys and hashes), so members can still use them, then set up a new authenticator; others need a reset link from a maintainer _(not yet tested)_. Do not count on this: back up the key. |
| `secrets/instance_ed25519` | The instance's signing key; its public key is published in `/.well-known/osfm.json`, reserved for exchanging data between instances later                                 | The instance gets a new identity                                                                                                                                                                                                                                                                                                                                   |
| `tiles/`                   | The basemaps                                                                                                                                                              | Can be extracted again with `deploy/tiles.sh` (from a newer build)                                                                                                                                                                                                                                                                                                 |

`secrets/setup_code` exists only until the first maintainer is created and need not be backed up.

**Always back up `pb_data/` and `secrets/` together, and restore them together.** A database restored next to a newly generated key loses all TOTP secrets: when OSFM starts with an existing database but has to generate a key, it logs `A NEW MASTER KEY WAS GENERATED FOR EXISTING DATA` (or the same for the signing key). Stop it and put the right key back. Keep the backups somewhere else than the server, and treat them as secret: `secrets/` decrypts the TOTP secrets in the database.

A one-click backup in the app (database, GeoJSON export of the places and recommendations, and the keys) is planned but not available yet.

### How

SQLite keeps recent changes in a write-ahead log next to the database (`-wal` files), so copying the files of a running instance can give an inconsistent copy. Either stop OSFM, or copy the database with SQLite's own tools.

**Stopped (simplest)**: a few seconds of downtime _(not yet verified on a server)_.

```bash
# Docker Compose: archive the volume with a throwaway container
docker compose stop osfm
docker run --rm -v osfm_osfm-data:/data:ro -v "$PWD":/backup busybox \
  tar -czf /backup/osfm-$(date -u +%Y%m%dT%H%M%SZ).tar.gz -C /data pb_data secrets
docker compose start osfm

# systemd
sudo systemctl stop osfm
sudo tar -czf osfm-$(date -u +%Y%m%dT%H%M%SZ).tar.gz -C /var/lib/osfm pb_data secrets
sudo systemctl start osfm
```

The compose example sets the project name `osfm`, and Compose prefixes the volume with it: `osfm_osfm-data`. `docker volume ls` shows the actual name.

**Running**: with the `sqlite3` command-line tool on the host, `VACUUM INTO` writes a consistent copy of the database while OSFM keeps running _(not yet verified with OSFM)_. Copy `secrets/` alongside it. `auxiliary.db` holds only the request logs and can be left out.

```bash
sudo sqlite3 /var/lib/osfm/pb_data/data.db "VACUUM INTO '/root/osfm-backup/data.db'"
sudo cp -a /var/lib/osfm/secrets /root/osfm-backup/
```

For the Docker volume, the files are under the volume's mount point on the host (`docker volume inspect osfm_osfm-data`).

**PocketBase's backups**: with the [admin UI](#admin-ui) turned on, its _Backups_ settings page creates, downloads and restores zip archives of `pb_data/` (stored in `pb_data/backups/`), and can create them on a schedule _(not yet verified with OSFM)_. They do **not** include `secrets/`; back that up separately. The pre-migration backups described under [Upgrades](#upgrades) appear in the same list.

### Restore, and test it

To restore: stop OSFM, replace `pb_data/` and `secrets/` in the data directory with the ones from the backup (with the right owner: user 65532 for the image, `osfm` for the systemd unit; `secrets/` mode 0700, the key files 0600: OSFM refuses to start with keys readable by others), and start it. For a copy made with `VACUUM INTO`, put it in place as `pb_data/data.db` and remove any old `data.db-wal` and `data.db-shm` files.

Test a backup before you need it, on another machine or in another directory _(not yet verified)_:

```bash
mkdir restore-test && tar -xzf osfm-20261201T030000Z.tar.gz -C restore-test
OSFM_DATA_DIR=$PWD/restore-test OSFM_LISTEN=127.0.0.1:18090 ./osfm serve
```

The log must not show `A NEW MASTER KEY WAS GENERATED FOR EXISTING DATA`. Then open `http://localhost:18090` and sign in with a password and authenticator app: that proves the key matches the database. (Passkeys only work at the instance's own host name, so they cannot be tested on such a copy.)

## Upgrades

1. **Back up first** (see above), and read the release notes on the [releases page](https://github.com/EatWell-Studio/OSFM/releases).
2. **Replace the program**:
   - Docker Compose: set the new tag in `compose.yml` (pin a version such as `1.2.3` or `1.2` rather than `latest`, so that upgrades happen when you choose), then `docker compose pull && docker compose up -d`.
   - systemd: verify and install the new binary as above, then `sudo systemctl restart osfm`.
3. **Migrations run on start.** There is no separate migrate command. Before applying migrations to an existing database, OSFM writes a backup of `pb_data/` to `pb_data/backups/premigrate_<UTC time>.zip`; if that backup fails, it does not start. These backups are not pruned automatically, stay on the same disk and do not include `secrets/`: they are a safety net, not a replacement for your own backups.
4. **Check the log** (see [Logs](#logs)): the configuration, any migration backup, and `serving; the admin UI is off`.

Going back to an older version is not automatic: an older version does not undo the newer one's migrations. Restore the pre-migration backup (or your own) and run the older version on it.

## Logs

OSFM writes to two places:

- **Its output** (`docker compose logs osfm`, or `journalctl -u osfm`): the effective configuration at every start, key generation and its warnings, the setup code, the addresses it serves on, and errors. The compose example rotates the container's output (3 × 10 MB).
- **PocketBase's log store** (in `pb_data/auxiliary.db`): the request log, kept for 7 days, without IP addresses, record IDs or query strings, and warnings logged while serving, such as missing trusted proxy headers, sign-in lockouts or a basemap file that cannot be read. These do not appear in the output; read them in the [admin UI](#admin-ui) under _Logs_.

OSFM sends no telemetry.

## Troubleshooting

### Everyone gets "too many attempts"

The sign-in rate limits and lockouts count per client address. If `OSFM_TRUSTED_PROXY_HEADERS` is not set, every request seems to come from the proxy, and a few failed attempts by anyone lock out everyone. Check the startup line `OSFM configuration`: it must show `trusted_proxy_headers=X-Forwarded-For`, not `(not set: PocketBase's setting applies)`. Set the variable and restart; the counters are only in memory, so a restart also clears current lockouts. (When the variable is unset, OSFM leaves PocketBase's own trusted proxy setting as it is, so an earlier value set in the admin UI may still apply.) Members who share one public address (for example on the same mobile network behind carrier-grade NAT) also share the per-client limits; how this works out in practice has not been evaluated yet.

### Passkeys fail

A passkey belongs to a host name (the relying party ID) and OSFM accepts passkey ceremonies only from the origin in `OSFM_PUBLIC_URL`: scheme, host name and port must match what the browser's address bar shows exactly (`https://food.example.org`, or `https://food.example.org:8443` on a high port). Check `passkey_rp_id` and `passkey_origins` in the startup line. Common causes: `OSFM_PUBLIC_URL` not set (OSFM then assumes `localhost`), opening the instance under another name, `www.` or an IP address, or a missing or extra port. Passkeys also do not work at an IP address at all.

Changing the host name later makes every existing passkey unusable: members then need their password and authenticator app, a recovery code, or a reset link from a maintainer.

### Signing in does not stick (the session cookie is not set)

The session cookie is `Secure`: browsers keep it only over HTTPS (or on `localhost`/loopback addresses). Opening the instance over plain HTTP, such as `http://<server>:8090` directly, lets nobody sign in. Use the HTTPS URL through the reverse proxy. OSFM refuses to start with an `http://` `OSFM_PUBLIC_URL` for any other host. With an `https://` URL the cookie is named `__Host-osfm_session`, which browsers accept only over HTTPS, without a `Domain` attribute and with `Path=/`; a proxy must not rewrite `Set-Cookie`.

### Other

- **The map shows that a city has no basemap yet**: the file `tiles/<slug>.pmtiles` is missing, has another name than the city's slug, or cannot be read by OSFM's user (the log store has a warning for files it cannot read).
- **The page stays blank or the console shows Content-Security-Policy violations**: something between the browser and OSFM injects scripts or styles, such as a CDN's optimisation or analytics features (see [Cloudflare](#cloudflare)) or a panel's content replacement. OSFM itself loads nothing from other origins.
- **The app's scripts or icons answer 404, or old versions stick**: the proxy serves or caches static files itself instead of passing everything to OSFM; see [nginx and control panels](#nginx-and-control-panels).
- **Caddy cannot get a certificate**: ports 80 and 443 must be reachable from the internet and the DNS records must point at the server; otherwise use the [DNS challenge](#home-connection-with-ports-80-and-443-blocked). Caddy's own log (`journalctl -u caddy`) says which challenge failed.
- **OSFM does not start**: its output lists every invalid variable at once, or the key or migration error.
