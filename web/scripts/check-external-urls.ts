// Fails when the built web app (dist/app) contains a URL of another origin: everything the app loads comes from the
// instance itself, with no CDN, web font, analytics or map service (docs/architecture.md §4.7). The content security
// policy enforces this in the browser; this check catches it at build time, before a page can try.
//
//   node scripts/check-external-urls.ts [dir]     (pnpm run check:urls, after pnpm run build; dir defaults to dist/app)
//
// It looks for absolute http(s) and ws(s) URLs anywhere in the text files, and for protocol-relative URLs (//host/…) in
// HTML attributes and CSS url(). Third-party bundles carry URLs that are never requested: XML namespace names, parse
// bases, documentation links in error messages, licence banners and link targets. Those are listed in `allowed` below,
// each matched exactly and with its reason; anything else fails the check. Licence and notice files are not scanned.
//
// Exit status: 0 when nothing is found, 1 when something is.

import { readdirSync, readFileSync } from 'node:fs'
import { extname, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

export interface Allowance {
  url: RegExp
  reason: string
}

export const allowed: Allowance[] = [
  {
    url: /^http:\/\/www\.w3\.org\/(2000\/svg|1999\/xlink|1999\/xhtml|1998\/Math\/MathML|XML\/1998\/namespace|2000\/xmlns\/?)$/,
    reason: 'XML namespace names (React DOM, MapLibre, inline SVG in CSS): identifiers, never fetched',
  },
  {
    url: /^http:\/\/localhost$/,
    reason: 'React Router: the base for parsing a path into a URL when there is no window; never fetched',
  },
  {
    url: /^https:\/\/[a-z0-9-]+\.invalid\/?$/,
    reason: 'parse bases under .invalid, a top-level domain reserved never to resolve (RFC 2606): OSFM, MapLibre',
  },
  {
    url: /^https:\/\/react\.dev\/errors\/$/,
    reason: 'React: the documentation link in production error messages',
  },
  {
    url: /^https:\/\/react\.i18next\.com\/latest\/usetranslation-hook$/,
    reason: 'react-i18next: a documentation link in an error message',
  },
  {
    url: /^https:\/\/reactrouter\.com\/en\/main\/routers\/picking-a-router$/,
    reason: 'React Router: a documentation link in an error message',
  },
  {
    url: /^https:\/\/github\.com\/ungap\/url-search-params$/,
    reason: 'React Router: a documentation link in an error message',
  },
  {
    url: /^https:\/\/github\.com\/mapbox\/mapbox-gl-js\/issues\/2907$/,
    reason: 'MapLibre: an issue link in a console warning',
  },
  {
    url: /^https:\/\/wiki\.openstreetmap\.org\/wiki\/This_map_requires_WebGL$/,
    reason: 'MapLibre: a documentation link in an error message',
  },
  {
    url: /^https:\/\/bit\.ly\/wb-precache$/,
    reason: 'Workbox: a documentation link in a console warning',
  },
  {
    url: /^https:\/\/tailwindcss\.com$/,
    reason: 'Tailwind CSS: the licence banner of the stylesheet',
  },
  {
    url: /^https:\/\/github\.com\/maplibre\/maplibre-gl-js\/blob\/v[0-9.]+\/LICENSE\.txt$/,
    reason: 'MapLibre: the licence banner of its worker',
  },
  {
    url: /^https:\/\/www\.openstreetmap\.org\/copyright$/,
    reason: 'the OpenStreetMap attribution link (required by the ODbL): a link the member may follow, never fetched',
  },
  {
    url: /^https:\/\/maplibre\.org\/$/,
    reason: "MapLibre's own attribution and logo controls: link targets, never fetched (the app does not show them)",
  },
]

// Licence texts and notices that ship with the map's glyphs and sprites: documents, never loaded by the app.
// Relative to the scanned directory, with / as the separator.
export const exemptFiles = ['map/NOTICE.txt', 'map/fonts/OFL.txt', 'map/sprites/LICENSE.md']

// Binary files: not scanned.
const binary = new Set(['.png', '.jpg', '.jpeg', '.gif', '.webp', '.ico', '.pbf', '.woff', '.woff2', '.wasm'])

// Absolute URLs with a scheme that loads something, and protocol-relative URLs where HTML or CSS would load them.
const absolute = /\b(?:https?|wss?):\/\/[^\s"'`<>()\\{}]+/gi
const protocolRelative = /(?:url\(\s*["']?|\b(?:src|href|action|srcset|poster|data)\s*=\s*["']?)(\/\/[^\s"'`<>()\\]+)/gi

export interface Finding {
  file: string
  line: number
  url: string
}

function files(dir: string): string[] {
  return readdirSync(dir, { recursive: true, withFileTypes: true })
    .filter((entry) => entry.isFile())
    .map((entry) => relative(dir, join(entry.parentPath, entry.name)).split(sep).join('/'))
    .sort()
}

function isAllowed(url: string): boolean {
  return allowed.some((a) => a.url.test(url))
}

/** Lists every URL of another origin in the text files under dir, outside the allow-list and the exempt files. */
export function findExternalUrls(dir: string): Finding[] {
  const findings: Finding[] = []
  for (const file of files(dir)) {
    if (exemptFiles.includes(file) || binary.has(extname(file).toLowerCase())) {
      continue
    }
    const lines = readFileSync(join(dir, file), 'utf8').split('\n')
    lines.forEach((text, index) => {
      const urls = [...text.matchAll(absolute)].map((m) => m[0])
      urls.push(...[...text.matchAll(protocolRelative)].map((m) => m[1] ?? ''))
      for (const raw of urls) {
        // Sentence punctuation after a URL in a message is not part of it.
        const url = raw.replace(/[.,;:!?]+$/, '')
        if (!isAllowed(url)) {
          findings.push({ file, line: index + 1, url })
        }
      }
    })
  }
  return findings
}

function main(): number {
  const dir = process.argv[2] ?? join(import.meta.dirname, '..', 'dist', 'app')
  let findings: Finding[]
  try {
    findings = findExternalUrls(dir)
  } catch (e) {
    console.error(`check-external-urls: cannot scan ${dir} (run pnpm run build first): ${String(e)}`)
    return 1
  }
  if (findings.length === 0) {
    console.log(`check-external-urls: no URLs of other origins in ${relative(process.cwd(), dir) || '.'}.`)
    return 0
  }
  console.error('URLs of other origins in the built web app; serve the resource from the instance instead (§4.7):')
  for (const f of findings) {
    console.error(`  ${relative(process.cwd(), join(dir, f.file))}:${String(f.line)}: ${f.url}`)
  }
  console.error('A URL that is never requested (a name, a link in a message) can be added to `allowed` in')
  console.error('web/scripts/check-external-urls.ts, with its reason.')
  return 1
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  process.exitCode = main()
}
