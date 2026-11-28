import { describe, expect, it } from 'vitest'
import {
  filterCount,
  type Filters,
  filtersKey,
  mapSearch,
  noFilters,
  parseFilters,
  toggled,
  writeFilters,
} from './filters'

const some: Filters = {
  slots: ['lunch', 'late_night'],
  categories: ['cafe'],
  cuisines: ['noodle', 'hot_pot'],
  by: ['Test Name, with a comma', 'Test Other'],
  mustTry: true,
  noAvoid: true,
}

describe('filters in the query string', () => {
  it('reads nothing from an empty query string', () => {
    expect(parseFilters(new URLSearchParams())).toEqual(noFilters)
    expect(filterCount(noFilters)).toBe(0)
  })

  it('writes the API parameter names and reads back what it wrote', () => {
    const params = writeFilters(new URLSearchParams(), some)
    expect(params.get('slots')).toBe('lunch,late_night')
    expect(params.get('category')).toBe('cafe')
    // In the list's order, whatever order they were chosen in.
    expect(params.get('cuisine')).toBe('hot_pot,noodle')
    // Each signature on its own: a name may contain a comma.
    expect(params.getAll('by')).toEqual(['Test Name, with a comma', 'Test Other'])
    expect(params.get('mustTry')).toBe('1')
    expect(params.get('noAvoid')).toBe('1')
    expect(parseFilters(params)).toEqual({ ...some, cuisines: ['hot_pot', 'noodle'] })
  })

  it('keeps the other parameters and replaces the old filters', () => {
    const params = new URLSearchParams('city=example-bay&slots=dinner&mustTry=1&other=x')
    writeFilters(params, { ...noFilters, categories: ['bar'] })
    expect(params.toString()).toBe('city=example-bay&other=x&category=bar')
  })

  it('takes repeated and comma-separated values, in the order of the day, each once', () => {
    const filters = parseFilters(new URLSearchParams('slots=dinner,breakfast&slots=dinner&slots=+lunch+'))
    expect(filters.slots).toEqual(['breakfast', 'lunch', 'dinner'])
  })

  it('ignores what the server would refuse', () => {
    const filters = parseFilters(
      new URLSearchParams(
        `slots=brunch,lunch&category=pharmacy&cuisine=NOODLE&by=&by=%20&by=${'x'.repeat(201)}&mustTry=yes&noAvoid=0`,
      ),
    )
    expect(filters).toEqual({ ...noFilters, slots: ['lunch'] })
  })

  it('trims signatures and keeps each once', () => {
    expect(parseFilters(new URLSearchParams('by=Test%20A&by=%20Test%20A%20&by=Test%20B')).by).toEqual([
      'Test A',
      'Test B',
    ])
  })

  it('counts every chosen value and each switch', () => {
    expect(filterCount(some)).toBe(2 + 1 + 2 + 2 + 1 + 1)
    expect(filterCount({ ...noFilters, noAvoid: true })).toBe(1)
  })

  it('gives the same key for the same filters', () => {
    expect(filtersKey({ ...noFilters, slots: ['dinner', 'lunch'] })).toBe(
      filtersKey({ ...noFilters, slots: ['lunch', 'dinner'] }),
    )
    expect(filtersKey(noFilters)).toBe('')
  })

  it('keeps only the city and the filters for the map and the list', () => {
    expect(mapSearch(new URLSearchParams('r=0190a1b2-0000-7000-8000-00000000c001&city=example-bay&slots=lunch'))).toBe(
      '?city=example-bay&slots=lunch',
    )
    expect(mapSearch(new URLSearchParams('r=x'))).toBe('')
  })

  it('turns a value on and off', () => {
    expect(toggled(['lunch'], 'dinner')).toEqual(['lunch', 'dinner'])
    expect(toggled(['lunch', 'dinner'], 'lunch')).toEqual(['dinner'])
  })
})
