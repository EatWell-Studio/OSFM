# syntax=docker/dockerfile:1
#
# The OSFM container image: the osfm binary (PocketBase core, OSFM API and the embedded web app) on distroless static.
# Build from the repository root:
#
#   docker buildx build -f deploy/Dockerfile --platform linux/amd64,linux/arm64 --build-arg VERSION=v1.2.3 .
#
# The web app and the Go binary are built on the build machine's own platform and cross-compiled for the target one
# (no emulation); only the final stage is per target platform, and it runs nothing.
#
# Data lives in one volume, /data (pb_data/, tiles/, secrets/), owned by the image's non-root user (65532).

# ---------------------------------------------------------------------------------------------------------------------
# Web app: web/dist/app
# ---------------------------------------------------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM node:24-trixie-slim AS web
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml ./
# The pnpm version is the one package.json pins in "packageManager" (as in CI), installed with npm rather than corepack.
RUN npm install --global --no-fund --no-audit "$(node -p 'require("./package.json").packageManager')" \
	&& pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---------------------------------------------------------------------------------------------------------------------
# Binary: cross-compiled for the target platform, pure Go (PocketBase's SQLite driver needs no CGO)
# ---------------------------------------------------------------------------------------------------------------------
# The Go version must be at least the one in go.mod: the golang image sets GOTOOLCHAIN=local and does not download one.
FROM --platform=$BUILDPLATFORM golang:1.27.4-trixie AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/app web/dist/app
ARG TARGETOS
ARG TARGETARCH
# The software version (osfm --version, /.well-known/osfm.json), e.g. the release tag v1.2.3. The build context has no
# .git, so without it the binary reports "dev".
ARG VERSION=""
RUN --mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
	go build -trimpath -ldflags "-s -w -X github.com/EatWell-Studio/OSFM/internal/version.version=${VERSION}" \
	-o /out/osfm ./cmd/osfm \
	&& mkdir /out/data

# ---------------------------------------------------------------------------------------------------------------------
# Runtime image
# ---------------------------------------------------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian13:nonroot

LABEL org.opencontainers.image.title="OSFM" \
	org.opencontainers.image.description="OpenStreetFoodMap: a food map for a circle of friends, on top of OpenStreetMap" \
	org.opencontainers.image.source="https://github.com/EatWell-Studio/OSFM" \
	org.opencontainers.image.url="https://github.com/EatWell-Studio/OSFM" \
	org.opencontainers.image.licenses="AGPL-3.0-or-later"

COPY --from=go /out/osfm /usr/local/bin/osfm
# An empty /data owned by the non-root user: a new named volume mounted there inherits this ownership.
COPY --from=go --chown=65532:65532 /out/data /data

USER 65532:65532
ENV OSFM_DATA_DIR=/data \
	OSFM_LISTEN=0.0.0.0:8090
VOLUME /data
EXPOSE 8090

ENTRYPOINT ["/usr/local/bin/osfm"]
# The explicit flags make the image work whether or not the binary reads OSFM_DATA_DIR and OSFM_LISTEN yet.
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/data/pb_data"]
