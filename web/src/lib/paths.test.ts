import { describe, expect, it } from 'vitest'
import { editRecommendationPath, isUuid, placePath, revisionsPath, shareLink } from './paths'

const place = '0190a1b2-0000-7000-8000-00000000b001'
const rec = '0190a1b2-0000-7000-8000-00000000c001'

describe('paths', () => {
  it('builds the share link of a place and of one recommendation', () => {
    expect(shareLink('https://food.example.org', place)).toBe(`https://food.example.org/p/${place}`)
    expect(shareLink('https://food.example.org', place, rec)).toBe(`https://food.example.org/p/${place}?r=${rec}`)
  })

  it('builds the paths of the routes', () => {
    expect(placePath(place)).toBe(`/p/${place}`)
    expect(editRecommendationPath(place, rec)).toBe(`/p/${place}/r/${rec}/edit`)
    expect(revisionsPath(place, rec)).toBe(`/p/${place}/r/${rec}/history`)
  })

  it('accepts only UUIDv7 as the server writes them', () => {
    expect(isUuid(place)).toBe(true)
    expect(isUuid(place.toUpperCase())).toBe(false)
    expect(isUuid('0190a1b2-0000-4000-8000-00000000b001')).toBe(false)
    expect(isUuid(undefined)).toBe(false)
  })
})
