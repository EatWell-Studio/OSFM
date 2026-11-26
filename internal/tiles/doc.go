// Package tiles reads the per-city PMTiles files and serves them as z/x/y tiles.
//
// Each city's basemap is one PMTiles v3 archive, <dir>/<city>.pmtiles, extracted at zoom levels 10 to 15 with
// deploy/tiles.sh (docs/architecture.md §3.6, AD7). [Handler] unpacks the archives and answers
//
//	/tiles/<city>/<z>/<x>/<y>.mvt   one vector tile, with the compression it is stored with as its Content-Encoding
//	/tiles/<city>.json              the city's TileJSON
//	/tiles/                         the index of the cities, with the bounds, center and zoom levels of their TileJSON:
//	                                {"cities": [{"id", "bounds", "center", "minzoom", "maxzoom"}, ...]}
//
// so that every tile is an ordinary cacheable request, rather than letting the browser send range requests into the
// archive. The index lists the cities the map page can switch between: a city is an archive in the tiles directory, so
// adding one takes nothing but its file. City names are limited to lowercase letters, digits and '-'; anything else, a
// missing archive and a tile outside the archive's zoom levels or area is answered with 404.
//
// The archives are read by a small reader of local PMTiles v3 files written from the specification (pmtiles.go),
// not with go-pmtiles: that library links the AWS, Azure and Google Cloud SDKs for its remote buckets, which doubles
// the binary, and panics on corrupt archives. This reader checks every offset and length against the file before it
// reads and bounds decompression, so a broken archive is answered with 404 and one log warning.
package tiles
