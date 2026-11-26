// @vitest-environment node
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { findExternalUrls } from './check-external-urls.ts'

// A made-up build: what the real one has (same-origin paths, allowed strings, licence files, binary files).
function writeBuild(dir: string) {
  mkdirSync(join(dir, 'assets'))
  mkdirSync(join(dir, 'map', 'fonts', 'Noto Sans Regular'), { recursive: true })
  writeFileSync(
    join(dir, 'index.html'),
    '<!doctype html><link rel="manifest" href="/manifest.webmanifest"><script type="module" src="/assets/index-AbCdEf12.js"></script>\n',
  )
  writeFileSync(
    join(dir, 'assets', 'index-AbCdEf12.js'),
    [
      'const ns="http://www.w3.org/2000/svg";var b=new URL(`http://localhost`);const base=`https://osfm.invalid`;',
      'throw Error(`See https://reactrouter.com/en/main/routers/picking-a-router.`);fetch("/api/osfm/me")',
      'var t=`https://react.dev/errors/`+e;const a=`© <a href="https://www.openstreetmap.org/copyright">OSM</a>`',
    ].join('\n'),
  )
  writeFileSync(
    join(dir, 'assets', 'index-AbCdEf12.css'),
    '/*! tailwindcss v4 | MIT License | https://tailwindcss.com */',
  )
  writeFileSync(join(dir, 'map', 'NOTICE.txt'), 'From https://github.com/protomaps/basemaps-assets\n')
  writeFileSync(join(dir, 'map', 'fonts', 'OFL.txt'), 'https://scripts.sil.org/OFL\n')
  writeFileSync(join(dir, 'map', 'fonts', 'Noto Sans Regular', '0-255.pbf'), 'https://example.com binary')
  writeFileSync(join(dir, 'manifest.webmanifest'), '{"start_url":"/","icons":[{"src":"/pwa-192x192.png"}]}')
}

describe('findExternalUrls', () => {
  let dir: string

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'osfm-urls-'))
    writeBuild(dir)
  })

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true })
  })

  it('accepts a build with same-origin URLs and allowed strings only', () => {
    expect(findExternalUrls(dir)).toEqual([])
  })

  it('reports a URL of another origin added to a bundle, a page or the manifest', () => {
    writeFileSync(join(dir, 'assets', 'extra-Zz9Yy8Xx.js'), 'import("https://cdn.example.com/lib.js")\n')
    writeFileSync(join(dir, 'other.html'), '<link href="https://fonts.example.com/css?family=X" rel="stylesheet">\n')
    writeFileSync(join(dir, 'manifest.webmanifest'), '{"icons":[{"src":"https://img.example.com/i.png"}]}')
    expect(findExternalUrls(dir)).toEqual([
      { file: 'assets/extra-Zz9Yy8Xx.js', line: 1, url: 'https://cdn.example.com/lib.js' },
      { file: 'manifest.webmanifest', line: 1, url: 'https://img.example.com/i.png' },
      { file: 'other.html', line: 1, url: 'https://fonts.example.com/css?family=X' },
    ])
  })

  it('reports protocol-relative URLs in HTML and CSS, websockets, and near misses of allowed URLs', () => {
    writeFileSync(join(dir, 'assets', 'x-Zz9Yy8Xx.css'), 'a{background:url(//cdn.example.com/bg.png)}\n')
    writeFileSync(join(dir, 'y.html'), "<script src='//cdn.example.com/a.js'></script>\n")
    writeFileSync(
      join(dir, 'assets', 'z-Zz9Yy8Xx.js'),
      'new WebSocket("wss://live.example.com/s");fetch(`http://localhost:8090/api`);fetch(`https://react.dev/errors/x`)\n',
    )
    expect(findExternalUrls(dir)).toEqual([
      { file: 'assets/x-Zz9Yy8Xx.css', line: 1, url: '//cdn.example.com/bg.png' },
      { file: 'assets/z-Zz9Yy8Xx.js', line: 1, url: 'wss://live.example.com/s' },
      { file: 'assets/z-Zz9Yy8Xx.js', line: 1, url: 'http://localhost:8090/api' },
      { file: 'assets/z-Zz9Yy8Xx.js', line: 1, url: 'https://react.dev/errors/x' },
      { file: 'y.html', line: 1, url: '//cdn.example.com/a.js' },
    ])
  })

  it('scans text files outside the exempt licence files, map/ included', () => {
    writeFileSync(join(dir, 'map', 'style.json'), '{"glyphs":"https://tiles.example.com/{fontstack}/{range}.pbf"}')
    writeFileSync(join(dir, 'map', 'README.txt'), 'see https://example.com/\n')
    expect(findExternalUrls(dir).map((f) => f.file)).toEqual(['map/README.txt', 'map/style.json'])
  })
})
