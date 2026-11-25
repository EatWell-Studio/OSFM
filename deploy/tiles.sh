#!/usr/bin/env bash
#
# Extracts the basemap of one city from the latest Protomaps daily OpenStreetMap build into <outdir>/<city>.pmtiles,
# zoom levels 10 to 15 (docs/architecture.md §3.6). The build (about 140 GB) is never downloaded as a whole: the pmtiles
# CLI reads only the parts it needs with HTTP range requests.
#
#   deploy/tiles.sh [--dry-run] <city> <minLon,minLat,maxLon,maxLat> [outdir]
#
#   <city>     the name the server knows the city by, used in /tiles/<city>/...: lowercase letters, digits and '-'
#   <bbox>     the city's bounding box in WGS-84 degrees
#   [outdir]   default: ${OSFM_DATA_DIR:-./data}/tiles
#   --dry-run  only estimate the size of the extract, write nothing
#
# Example (a made-up name with a small bounding box on the equator; use your own city's):
#
#   deploy/tiles.sh example-town 6.68,0.30,6.76,0.36
#
# Requires curl and the pmtiles CLI (https://github.com/protomaps/go-pmtiles/releases, or `brew install pmtiles`).
# OSFM_TILES_SOURCE overrides the source archive (a URL or a local .pmtiles file) instead of the latest daily build.
#
# The extract is written to a hidden temporary file first and renamed into place when complete, so a running server
# never sees a partial file. Exit status: 0 on success, 2 on invalid arguments, 1 on any other failure.

set -euo pipefail

usage() {
	cat >&2 <<'EOF'
usage: deploy/tiles.sh [--dry-run] <city> <minLon,minLat,maxLon,maxLat> [outdir]

  <city>     lowercase letters, digits and '-', e.g. example-town
  <bbox>     the city's bounding box in WGS-84 degrees, e.g. 6.68,0.30,6.76,0.36
  [outdir]   default: ${OSFM_DATA_DIR:-./data}/tiles
  --dry-run  only estimate the size of the extract, write nothing
EOF
	exit 2
}

dry_run=0
if [ "${1:-}" = "--dry-run" ]; then
	dry_run=1
	shift
fi
[ "$#" -ge 2 ] && [ "$#" -le 3 ] || usage

city="$1"
bbox="$2"
outdir="${3:-${OSFM_DATA_DIR:-./data}/tiles}"

if ! printf '%s' "$city" | grep -Eq '^[a-z0-9-]{1,64}$'; then
	echo "tiles: invalid city name '$city': use 1 to 64 lowercase letters, digits and '-'" >&2
	exit 2
fi

number='-?[0-9]{1,3}(\.[0-9]+)?'
if ! printf '%s' "$bbox" | grep -Eq "^$number,$number,$number,$number\$"; then
	echo "tiles: invalid bounding box '$bbox': expected minLon,minLat,maxLon,maxLat in degrees" >&2
	exit 2
fi
# Web Mercator tiles end at ±85.0511° latitude.
if ! printf '%s' "$bbox" | awk -F, '{ exit !($1 >= -180 && $3 <= 180 && $1 < $3 && $2 >= -85.0511 && $4 <= 85.0511 && $2 < $4) }'; then
	echo "tiles: invalid bounding box '$bbox': need -180 <= minLon < maxLon <= 180 and -85.0511 <= minLat < maxLat <= 85.0511" >&2
	exit 2
fi

for tool in curl pmtiles; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "tiles: '$tool' not found in PATH" >&2
		exit 1
	fi
done

# The latest daily build: today's may not be published yet, so look back up to two weeks.
day_before() { # <days back> → YYYYMMDD (UTC); GNU date first, then BSD/macOS date
	date -u -d "-$1 day" +%Y%m%d 2>/dev/null || date -u -v-"$1"d +%Y%m%d
}
source="${OSFM_TILES_SOURCE:-}"
if [ -z "$source" ]; then
	for days in $(seq 0 14); do
		candidate="https://build.protomaps.com/$(day_before "$days").pmtiles"
		if curl -sfI --max-time 30 -o /dev/null "$candidate"; then
			source="$candidate"
			break
		fi
	done
	if [ -z "$source" ]; then
		echo "tiles: no Protomaps build found in the last 14 days at https://build.protomaps.com/" >&2
		exit 1
	fi
fi
echo "tiles: source $source"
echo "tiles: extracting $city, bbox $bbox, zoom 10-15"

if [ "$dry_run" -eq 1 ]; then
	pmtiles extract "$source" "$city.pmtiles" --bbox="$bbox" --minzoom=10 --maxzoom=15 --dry-run
	exit 0
fi

mkdir -p "$outdir"
target="$outdir/$city.pmtiles"
partial="$outdir/.$city.partial.pmtiles"
trap 'rm -f "$partial"' EXIT
pmtiles extract "$source" "$partial" --bbox="$bbox" --minzoom=10 --maxzoom=15
mv -f "$partial" "$target"
trap - EXIT

bytes="$(wc -c <"$target" | tr -d ' ')"
echo "tiles: wrote $target ($bytes bytes, $(awk -v b="$bytes" 'BEGIN { printf "%.1f MB", b / 1000000 }'))"
