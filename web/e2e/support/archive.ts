// A tiny basemap archive for the end-to-end tests, built in code so that no tile file is committed (*.pmtiles is kept
// out of the repository). It is a valid PMTiles v3 archive of Mapbox Vector Tiles over a made-up city: every tile holds
// the same made-up land, a lake and a road, enough for the basemap style to draw something. The output depends only on
// the arguments, so every run serves the same bytes.

/** minLon, minLat, maxLon, maxLat in WGS-84 degrees. */
export type BBox = [number, number, number, number]

export interface ArchiveSpec {
  bbox: BBox
  minZoom: number
  maxZoom: number
}

// Protocol buffers, as much as a vector tile needs.

class Writer {
  private bytes: number[] = []

  varint(value: number): this {
    let v = value
    while (v >= 0x80) {
      this.bytes.push((v & 0x7f) | 0x80)
      v = Math.floor(v / 128)
    }
    this.bytes.push(v)
    return this
  }

  key(field: number, wireType: number): this {
    return this.varint(field * 8 + wireType)
  }

  /** A varint field. */
  uint(field: number, value: number): this {
    return this.key(field, 0).varint(value)
  }

  /** A length-delimited field. */
  bytesField(field: number, value: Uint8Array): this {
    this.key(field, 2).varint(value.length)
    for (const b of value) {
      this.bytes.push(b)
    }
    return this
  }

  string(field: number, value: string): this {
    return this.bytesField(field, new TextEncoder().encode(value))
  }

  /** A packed repeated varint field. */
  packed(field: number, values: number[]): this {
    const inner = new Writer()
    for (const v of values) {
      inner.varint(v)
    }
    return this.bytesField(field, inner.finish())
  }

  finish(): Uint8Array {
    return Uint8Array.from(this.bytes)
  }
}

// Mapbox Vector Tile 2.1: geometry commands with zigzag-encoded deltas, in tile coordinates (0–4096, y down).

const extent = 4096
const zigzag = (n: number) => (n << 1) ^ (n >> 31)
const command = (id: number, count: number) => (id & 0x7) | (count << 3)
const moveTo = 1
const lineTo = 2
const closePath = 7

function lineGeometry(points: [number, number][]): number[] {
  const out: number[] = []
  let x = 0
  let y = 0
  points.forEach(([px, py], i) => {
    if (i === 0) {
      out.push(command(moveTo, 1))
    } else if (i === 1) {
      out.push(command(lineTo, points.length - 1))
    }
    out.push(zigzag(px - x), zigzag(py - y))
    x = px
    y = py
  })
  return out
}

/** A ring that is clockwise on screen (positive area with y down): an exterior ring by the specification. */
function polygonGeometry(ring: [number, number][]): number[] {
  return [...lineGeometry(ring), command(closePath, 1)]
}

const geometryType = { line: 2, polygon: 3 } as const

interface Feature {
  type: keyof typeof geometryType
  geometry: number[]
  kind?: string
}

function layer(name: string, features: Feature[]): Uint8Array {
  const w = new Writer().uint(15, 2).string(1, name)
  const kinds = [...new Set(features.flatMap((f) => (f.kind ? [f.kind] : [])))]
  features.forEach((f, i) => {
    const feature = new Writer().uint(1, i + 1)
    if (f.kind) {
      feature.packed(2, [0, kinds.indexOf(f.kind)])
    }
    feature.uint(3, geometryType[f.type]).packed(4, f.geometry)
    w.bytesField(2, feature.finish())
  })
  if (kinds.length > 0) {
    w.string(3, 'kind')
    for (const kind of kinds) {
      w.bytesField(4, new Writer().string(1, kind).finish())
    }
  }
  return w.uint(5, extent).finish()
}

/** The one tile every position of the archive holds: land, a lake in the middle and a road across. */
export function tileContent(): Uint8Array {
  const square = (a: number, b: number): [number, number][] => [
    [a, a],
    [b, a],
    [b, b],
    [a, b],
  ]
  const tile = new Writer()
  tile.bytesField(3, layer('earth', [{ type: 'polygon', geometry: polygonGeometry(square(0, extent)) }]))
  tile.bytesField(3, layer('water', [{ type: 'polygon', kind: 'lake', geometry: polygonGeometry(square(1536, 2560)) }]))
  tile.bytesField(
    3,
    layer('roads', [
      {
        type: 'line',
        kind: 'major_road',
        geometry: lineGeometry([
          [0, 3072],
          [extent, 3072],
        ]),
      },
    ]),
  )
  return tile.finish()
}

// PMTiles v3 (https://github.com/protomaps/PMTiles/blob/main/spec/v3/spec.md).

/** The tile ID of z/x/y: tiles of lower zoom levels first, then the position on the Hilbert curve. */
export function tileID(z: number, x: number, y: number): number {
  let id = 0
  for (let level = 0; level < z; level++) {
    id += 4 ** level
  }
  const n = 2 ** z
  let tx = x
  let ty = y
  for (let s = n / 2; s >= 1; s /= 2) {
    const rx = (tx & s) !== 0 ? 1 : 0
    const ry = (ty & s) !== 0 ? 1 : 0
    id += s * s * ((3 * rx) ^ ry)
    if (ry === 0) {
      if (rx === 1) {
        tx = n - 1 - tx
        ty = n - 1 - ty
      }
      ;[tx, ty] = [ty, tx]
    }
  }
  return id
}

function tileX(lon: number, z: number): number {
  return Math.floor(((lon + 180) / 360) * 2 ** z)
}

function tileY(lat: number, z: number): number {
  const rad = (lat * Math.PI) / 180
  return Math.floor(((1 - Math.log(Math.tan(rad) + 1 / Math.cos(rad)) / Math.PI) / 2) * 2 ** z)
}

/** The tile IDs of every tile that overlaps bbox at the zoom levels of spec, in increasing order. */
export function tileIDs(spec: ArchiveSpec): number[] {
  const [minLon, minLat, maxLon, maxLat] = spec.bbox
  const ids: number[] = []
  for (let z = spec.minZoom; z <= spec.maxZoom; z++) {
    for (let x = tileX(minLon, z); x <= tileX(maxLon, z); x++) {
      for (let y = tileY(maxLat, z); y <= tileY(minLat, z); y++) {
        ids.push(tileID(z, x, y))
      }
    }
  }
  return ids.sort((a, b) => a - b)
}

const headerLength = 127
const compressionNone = 1
const tileTypeMVT = 1

/** Builds the archive: one tile content, stored once, at every tile of the bbox (runs of consecutive IDs merged). */
export function buildArchive(spec: ArchiveSpec): Uint8Array {
  const tile = tileContent()
  const runs: { id: number; length: number }[] = []
  const ids = tileIDs(spec)
  for (const id of ids) {
    const last = runs.at(-1)
    if (last && last.id + last.length === id) {
      last.length++
    } else {
      runs.push({ id, length: 1 })
    }
  }
  // The root directory: count, tile ID deltas, run lengths, lengths, offsets + 1 (every entry points at offset 0).
  const dir = new Writer().varint(runs.length)
  let previous = 0
  for (const run of runs) {
    dir.varint(run.id - previous)
    previous = run.id
  }
  runs.forEach((run) => dir.varint(run.length))
  runs.forEach(() => dir.varint(tile.length))
  runs.forEach(() => dir.varint(1))
  const root = dir.finish()
  const metadata = new TextEncoder().encode(
    JSON.stringify({
      name: 'osfm-e2e',
      description: 'Made-up tiles for the end-to-end tests',
      attribution: 'Made-up test data',
      vector_layers: ['earth', 'water', 'roads'].map((id) => ({ id, fields: { kind: 'String' } })),
    }),
  )

  const out = new Uint8Array(headerLength + root.length + metadata.length + tile.length)
  const view = new DataView(out.buffer)
  out.set(new TextEncoder().encode('PMTiles'), 0)
  out[7] = 3
  let offset = headerLength
  const section = (at: number, length: number) => {
    view.setBigUint64(at, BigInt(offset), true)
    view.setBigUint64(at + 8, BigInt(length), true)
    offset += length
  }
  section(8, root.length) // root directory
  section(24, metadata.length) // metadata
  section(40, 0) // leaf directories: none
  section(56, tile.length) // tile data
  view.setBigUint64(72, BigInt(ids.length), true) // addressed tiles
  view.setBigUint64(80, BigInt(runs.length), true) // tile entries
  view.setBigUint64(88, 1n, true) // tile contents
  out[96] = 0 // not clustered: every entry points at the same content
  out[97] = compressionNone // directories and metadata
  out[98] = compressionNone // tiles
  out[99] = tileTypeMVT
  out[100] = spec.minZoom
  out[101] = spec.maxZoom
  const [minLon, minLat, maxLon, maxLat] = spec.bbox
  const e7 = (deg: number) => Math.round(deg * 1e7)
  view.setInt32(102, e7(minLon), true)
  view.setInt32(106, e7(minLat), true)
  view.setInt32(110, e7(maxLon), true)
  view.setInt32(114, e7(maxLat), true)
  out[118] = spec.maxZoom
  view.setInt32(119, e7((minLon + maxLon) / 2), true)
  view.setInt32(123, e7((minLat + maxLat) / 2), true)

  out.set(root, headerLength)
  out.set(metadata, headerLength + root.length)
  out.set(tile, headerLength + root.length + metadata.length)
  return out
}
