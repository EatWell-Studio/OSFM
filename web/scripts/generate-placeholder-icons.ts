// Writes the placeholder app icons to public/: a white dot on the theme colour, as PNG files, using only Node's zlib.
// The icons are committed; run this again only to change them:
//
//   node scripts/generate-placeholder-icons.ts
//
// Real artwork can replace the files later under the same names (vite.config.ts and index.html refer to them).

import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { deflateSync } from 'node:zlib'

const background: RGB = [0xc2, 0x41, 0x0c] // the manifest's theme colour, #c2410c
const foreground: RGB = [0xff, 0xff, 0xff]

type RGB = readonly [number, number, number]

interface Icon {
  file: string
  size: number
  // Radius of the dot as a fraction of the size. Maskable icons keep it inside the safe zone (a centred circle of 40 %
  // of the size in radius), since the platform may crop everything else.
  dot: number
}

const icons: Icon[] = [
  { file: 'pwa-192x192.png', size: 192, dot: 0.3 },
  { file: 'pwa-512x512.png', size: 512, dot: 0.3 },
  { file: 'maskable-icon-512x512.png', size: 512, dot: 0.22 },
  { file: 'apple-touch-icon-180x180.png', size: 180, dot: 0.26 },
  { file: 'favicon-48x48.png', size: 48, dot: 0.3 },
]

const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n
  for (let k = 0; k < 8; k++) {
    c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
  }
  return c >>> 0
})

function crc32(data: Buffer): number {
  let c = 0xffffffff
  for (const byte of data) {
    c = (crcTable[(c ^ byte) & 0xff] ?? 0) ^ (c >>> 8)
  }
  return (c ^ 0xffffffff) >>> 0
}

function chunk(type: string, data: Buffer): Buffer {
  const length = Buffer.alloc(4)
  length.writeUInt32BE(data.length)
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data])
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(crc32(body))
  return Buffer.concat([length, body, crc])
}

function png({ size, dot }: Icon): Buffer {
  const header = Buffer.alloc(13)
  header.writeUInt32BE(size, 0)
  header.writeUInt32BE(size, 4)
  header[8] = 8 // bit depth
  header[9] = 2 // colour type: RGB (opaque, as iOS wants for home screen icons)
  // Compression, filter and interlace methods stay 0.

  const row = 1 + size * 3
  const pixels = Buffer.alloc(row * size)
  const centre = size / 2
  const radius = size * dot
  for (let y = 0; y < size; y++) {
    pixels[y * row] = 0 // filter: none
    for (let x = 0; x < size; x++) {
      // Coverage of the pixel by the dot, sampled 4 × 4 for smooth edges.
      let inside = 0
      for (let sy = 0; sy < 4; sy++) {
        for (let sx = 0; sx < 4; sx++) {
          const dx = x + (sx + 0.5) / 4 - centre
          const dy = y + (sy + 0.5) / 4 - centre
          if (dx * dx + dy * dy <= radius * radius) {
            inside++
          }
        }
      }
      const a = inside / 16
      for (let ch = 0; ch < 3; ch++) {
        pixels[y * row + 1 + x * 3 + ch] = Math.round((foreground[ch] ?? 0) * a + (background[ch] ?? 0) * (1 - a))
      }
    }
  }
  const signature = Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
  return Buffer.concat([
    signature,
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(pixels, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ])
}

const out = join(import.meta.dirname, '..', 'public')
for (const icon of icons) {
  writeFileSync(join(out, icon.file), png(icon))
  console.log(`wrote public/${icon.file}`)
}
