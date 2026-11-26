// Fails when a .ts or .tsx file under src/ contains CJK characters outside the locale files: every visible string has
// to go through a translation key (docs/architecture.md §4.1). Comments count too, so that code stays English.
//
//   node scripts/check-hardcoded-cjk.ts [dir]     (pnpm run check:i18n; dir defaults to src/ beside this script)
//
// Exit status: 0 when nothing is found, 1 when something is.

import { readdirSync, readFileSync } from 'node:fs'
import { join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

// Han ideographs, kana, Hangul, Bopomofo, CJK symbols and punctuation, and full-width forms (e.g. U+FF0C, the comma).
// Built from a string so that the ranges stay escaped (a formatter may otherwise turn them into the characters).
const cjk = new RegExp(
  '[\\p{Script=Han}\\p{Script=Hiragana}\\p{Script=Katakana}\\p{Script=Hangul}\\p{Script=Bopomofo}' +
    '\\u3000-\\u303f\\uff00-\\uffef]',
  'u',
)

// Relative to the scanned directory. Locale files hold the translations; they are JSON today, but the directory is
// exempt whatever the file type.
const exempt = [join('i18n', 'locales') + sep]

export interface Finding {
  file: string
  line: number
  column: number
  text: string
}

function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { recursive: true, encoding: 'utf8' })
    .filter((path) => /\.(ts|tsx)$/.test(path))
    .sort()
}

/** Lists every line of a .ts/.tsx file under dir (outside the locale files) that contains a CJK character. */
export function findHardcodedCjk(dir: string): Finding[] {
  const findings: Finding[] = []
  for (const path of sourceFiles(dir)) {
    if (exempt.some((prefix) => path.startsWith(prefix))) {
      continue
    }
    const lines = readFileSync(join(dir, path), 'utf8').split('\n')
    lines.forEach((text, index) => {
      const match = cjk.exec(text)
      if (match) {
        findings.push({ file: path, line: index + 1, column: match.index + 1, text: text.trim() })
      }
    })
  }
  return findings
}

function main(): number {
  const dir = process.argv[2] ?? join(import.meta.dirname, '..', 'src')
  const findings = findHardcodedCjk(dir)
  if (findings.length === 0) {
    console.log('check-hardcoded-cjk: no CJK text outside the locale files.')
    return 0
  }
  console.error('CJK text outside the locale files; move it into src/i18n/locales/ and use a translation key:')
  for (const f of findings) {
    console.error(`  ${relative(process.cwd(), join(dir, f.file))}:${String(f.line)}:${String(f.column)}: ${f.text}`)
  }
  return 1
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  process.exitCode = main()
}
