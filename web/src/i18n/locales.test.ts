import { describe, expect, it } from 'vitest'
import { en, namespaces, zhCN } from './resources'

function keys(value: unknown, prefix = ''): string[] {
  if (typeof value !== 'object' || value === null) {
    return [prefix]
  }
  return Object.entries(value).flatMap(([k, v]) => keys(v, prefix ? `${prefix}.${k}` : k))
}

// Every locale file there is, registered or not.
const localeFiles = Object.keys(import.meta.glob('./locales/*/*.json'))

function files(language: string): string[] {
  const prefix = `./locales/${language}/`
  return localeFiles
    .filter((f) => f.startsWith(prefix))
    .map((f) => f.slice(prefix.length).replace(/\.json$/, ''))
    .sort()
}

describe('locales', () => {
  it('has one file per registered namespace in each language', () => {
    const registered = [...namespaces].sort()
    expect(files('zh-CN')).toEqual(registered)
    expect(files('en')).toEqual(registered)
  })

  it.each(namespaces)('en has exactly the keys of zh-CN in %s', (ns) => {
    expect(keys(en[ns]).sort()).toEqual(keys(zhCN[ns]).sort())
  })

  it('has no empty texts', () => {
    for (const [language, resources] of [
      ['zh-CN', zhCN],
      ['en', en],
    ] as const) {
      for (const ns of namespaces) {
        const empty = keys(resources[ns]).filter((k) => {
          const value = k.split('.').reduce<unknown>((v, part) => (v as Record<string, unknown>)[part], resources[ns])
          return typeof value !== 'string' || value.trim() === ''
        })
        expect(empty, `${language}/${ns}`).toEqual([])
      }
    }
  })
})
