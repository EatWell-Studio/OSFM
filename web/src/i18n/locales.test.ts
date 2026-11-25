import { describe, expect, it } from 'vitest'
import en from './locales/en.json'
import zhCN from './locales/zh-CN.json'

function keys(value: unknown, prefix = ''): string[] {
  if (typeof value !== 'object' || value === null) {
    return [prefix]
  }
  return Object.entries(value).flatMap(([k, v]) => keys(v, prefix ? `${prefix}.${k}` : k))
}

describe('locales', () => {
  it('en has exactly the keys of zh-CN', () => {
    expect(keys(en).sort()).toEqual(keys(zhCN).sort())
  })
})
