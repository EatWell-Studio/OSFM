// Package tiles reads the per-city PMTiles files and serves them as z/x/y tiles.
//
// Each city's basemap is one PMTiles v3 archive, <dir>/<city>.pmtiles, extracted at zoom levels 10 to 15 with
// deploy/tiles.sh (docs/architecture.md §3.6, AD7). [Handler] unpacks the archives with go-pmtiles and answers
//
//	/tiles/<city>/<z>/<x>/<y>.mvt   one vector tile, gzip-encoded as stored in the archive
//	/tiles/<city>.json              the city's TileJSON
//
// so that every tile is an ordinary cacheable request, rather than letting the browser send range requests into the
// archive. City names are limited to lowercase letters, digits and '-'; anything else, a missing archive and a tile
// outside the archive's zoom levels or area is answered with 404.
package tiles
