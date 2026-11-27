import { describe, expect, it } from 'vitest'
import { en, zhCN } from '@/i18n/resources'
import { enumLabels } from './enums'

describe('enum labels', () => {
  it.each(Object.entries(enumLabels))('%s has exactly one label per value, in both languages', (key, values) => {
    const labels = (zhCN.common as unknown as Record<string, Record<string, string>>)[key]
    const enLabels = (en.common as unknown as Record<string, Record<string, string>>)[key]
    expect(Object.keys(labels ?? {}).sort()).toEqual([...values].sort())
    expect(Object.keys(enLabels ?? {}).sort()).toEqual([...values].sort())
    expect(new Set(values).size).toBe(values.length)
  })

  it('has the eleven categories of the server', () => {
    expect(enumLabels.category).toHaveLength(11)
  })
})
