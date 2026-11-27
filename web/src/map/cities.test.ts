import { describe, expect, it } from 'vitest'
import { type City, cityLabel, contains, mergeCities, parseCities, pickCity } from './cities'

const town: City = {
  id: 'example-town',
  bounds: [6.68, 0.3, 6.76, 0.36],
  center: [6.72, 0.33, 10],
  minzoom: 10,
  maxzoom: 15,
}
const village: City = { id: 'village2', bounds: [1, 2, 3, 4], center: [2, 3, 10], minzoom: 10, maxzoom: 15 }

describe('parseCities', () => {
  it('keeps the valid entries and leaves out malformed ones', () => {
    const body = {
      cities: [
        town,
        { ...village, id: 'Bad Name' },
        { ...village, bounds: [3, 2, 1, 4] }, // west of east
        { ...village, bounds: [1, 2, 3] },
        { ...village, center: [2, 3, Number.NaN] },
        { ...village, minzoom: '10' },
        null,
        village,
      ],
    }
    expect(parseCities(JSON.parse(JSON.stringify(body)))).toEqual([town, village])
  })

  it('rejects a body that is not an index', () => {
    expect(() => parseCities([town])).toThrow()
    expect(() => parseCities(null)).toThrow()
    expect(() => parseCities('<!doctype html>')).toThrow()
  })
})

describe('pickCity', () => {
  const cities = [town, village]
  it('prefers the requested city, then the remembered one, then the first', () => {
    expect(pickCity(cities, 'village2', 'example-town')).toBe(village)
    expect(pickCity(cities, 'elsewhere', 'village2')).toBe(village)
    expect(pickCity(cities, null, 'elsewhere')).toBe(town)
    expect(pickCity<City>([], null, null)).toBeUndefined()
  })
})

describe('cityLabel', () => {
  it('capitalises the words of the id', () => {
    expect(cityLabel(town)).toBe('Example Town')
    expect(cityLabel(village)).toBe('Village2')
  })
})

describe('contains', () => {
  it('includes the edges and nothing outside', () => {
    expect(contains(town.bounds, 6.7, 0.33)).toBe(true)
    expect(contains(town.bounds, 6.68, 0.36)).toBe(true)
    expect(contains(town.bounds, 6.67, 0.33)).toBe(false)
    expect(contains(town.bounds, 6.7, 0.37)).toBe(false)
  })
})

describe('mergeCities', () => {
  it('offers the cities of the settings by name, then the other archives', () => {
    const configured = [
      { slug: 'test-village', name: 'Test Village', bbox: [7.1, 0.2, 7.2, 0.3], center: [7.15, 0.25] },
      { slug: 'example-town', name: 'Test Town', bbox: town.bounds, center: [town.center[0], town.center[1]] },
    ] satisfies Parameters<typeof mergeCities>[1]
    expect(mergeCities([town, village], configured)).toEqual([
      { id: 'test-village', label: 'Test Village', tiles: undefined },
      { id: 'example-town', label: 'Test Town', tiles: town },
      { id: 'village2', label: 'Village2', tiles: village },
    ])
    expect(mergeCities([], [])).toEqual([])
  })
})
