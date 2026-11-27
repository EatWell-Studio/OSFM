// @vitest-environment node
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { findHardcodedCjk } from './check-hardcoded-cjk.ts'

// The CJK samples are built from code points so that this file itself has none.
const han = String.fromCodePoint(0x5730, 0x56fe) // "map"
const fullWidthComma = String.fromCodePoint(0xff0c)

describe('findHardcodedCjk', () => {
  let dir: string

  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), 'osfm-cjk-'))
    mkdirSync(join(dir, 'i18n', 'locales'), { recursive: true })
    mkdirSync(join(dir, 'pages'))
  })

  afterEach(() => {
    rmSync(dir, { recursive: true, force: true })
  })

  it('reports CJK text in .ts and .tsx files', () => {
    writeFileSync(join(dir, 'pages', 'Bad.tsx'), `export const A = () => <h1>${han}</h1>\n`)
    writeFileSync(join(dir, 'bad.ts'), `// fine\nexport const s = 'a${fullWidthComma}b'\n`)
    expect(findHardcodedCjk(dir)).toEqual([
      { file: 'bad.ts', line: 2, column: 20, text: `export const s = 'a${fullWidthComma}b'` },
      { file: join('pages', 'Bad.tsx'), line: 1, column: 28, text: `export const A = () => <h1>${han}</h1>` },
    ])
  })

  it('ignores the locale files and other file types', () => {
    writeFileSync(join(dir, 'i18n', 'locales', 'zh-CN.ts'), `export default { map: '${han}' }\n`)
    writeFileSync(join(dir, 'i18n', 'locales', 'zh-CN.json'), `{ "map": "${han}" }\n`)
    // One file per namespace and language (src/i18n/resources.ts).
    mkdirSync(join(dir, 'i18n', 'locales', 'zh-CN'))
    writeFileSync(join(dir, 'i18n', 'locales', 'zh-CN', 'common.json'), `{ "map": "${han}" }\n`)
    writeFileSync(join(dir, 'i18n', 'locales', 'zh-CN', 'index.ts'), `export const map = '${han}'\n`)
    writeFileSync(join(dir, 'notes.md'), `${han}\n`)
    writeFileSync(join(dir, 'pages', 'Good.tsx'), `export const A = () => <h1>{t('nav.map')}</h1>\n`)
    expect(findHardcodedCjk(dir)).toEqual([])
  })

  it('finds nothing in the app sources', () => {
    expect(findHardcodedCjk(join(import.meta.dirname, '..', 'src'))).toEqual([])
  })
})
