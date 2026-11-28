// Package backup writes and restores OSFM backups (G5, T042): one zip file that holds everything needed to bring an
// instance back, and only that.
//
// # Layout
//
//	data.db                        ┐ the PocketBase data directory (pb_data) in PocketBase's own backup layout: a
//	auxiliary.db                   │ consistent copy of each database made with VACUUM INTO, and the other files of
//	storage/…                      ┘ pb_data except backups and temporary files
//	osfm/secrets/master.key        the instance keys from <data>/secrets
//	osfm/secrets/instance_ed25519
//	osfm/places.geojson            the circle's layers as the per-layer export writes them (internal/export), for
//	osfm/recommendations.geojson   reading without OSFM; a restore does not use them
//	osfm/manifest.json             [Manifest]: format, versions, time, and every other file with its size and SHA-256
//
// Because the pb_data part is laid out as PocketBase lays out its backups, PocketBase's own restore (admin UI) puts the
// database back correctly, though it also copies osfm/ into pb_data; `osfm restore` is the supported way.
//
// # Consistency and memory
//
// Both databases are copied with VACUUM INTO from a connection of their own, so a backup can be taken while the
// instance runs (from the admin page) or while it is stopped (`osfm backup`), and the copy is a point in time. The
// layers are exported from that same copy, opened as a separate PocketBase app, so they match the database in the zip
// exactly. Every file is streamed into the zip: memory use does not grow with the size of the database (a 2-core ARM
// board must manage), but the data directory's disk needs room for a copy of the database besides the zip.
//
// # The backup holds the master key
//
// The master key decrypts the members' TOTP secrets in the database, so a backup is as sensitive as the key itself.
// Backup files are created with mode 0600; the admin routes need a fresh proof to create, download or delete one and
// audit each of them. Nothing here logs key material.
package backup
