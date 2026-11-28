// The map filters (A3): time slots, categories, cuisines, signatures, "someone says must try" and "nobody says avoid".
// They live in the address's query string, so that the map and the list (A6), a reload and a link all show the same
// places; the parameters are named as those of GET /api/osfm/places (internal/api/map.go), which takes them as they
// are. Different conditions combine with "and", the values of one condition with "or" (the server's semantics).
//
// Nothing here sorts or ranks by attitude (C11): the filters only keep or leave out places.

import {
  MapParamBy,
  MapParamCategory,
  MapParamCity,
  MapParamCuisine,
  MapParamMustTry,
  MapParamNoAvoid,
  MapParamSlots,
} from '@/api/types.gen'
import { type Category, categories, type Cuisine, cuisines, type TimeSlot, timeSlots } from '@/lib/enums'

export interface Filters {
  slots: TimeSlot[]
  categories: Category[]
  cuisines: Cuisine[]
  /** Signatures, as GET /api/osfm/signatures lists them; matched exactly by the server. */
  by: string[]
  mustTry: boolean
  noAvoid: boolean
}

export const noFilters: Filters = { slots: [], categories: [], cuisines: [], by: [], mustTry: false, noAvoid: false }

/** The query parameters the filters use, besides the city, which the map and the list share too. */
export const filterParams = [
  MapParamSlots,
  MapParamCategory,
  MapParamCuisine,
  MapParamBy,
  MapParamMustTry,
  MapParamNoAvoid,
] as const

/** The longest signature kept from an address; longer values are no signature the server would list. */
const maxSignatureLength = 200

/**
 * The values of a parameter given once or repeated, each split at commas; empty parts are dropped. Only the fixed
 * values are split: a signature may contain a comma.
 */
function listParam(params: URLSearchParams, name: string): string[] {
  return params
    .getAll(name)
    .flatMap((v) => v.split(','))
    .map((v) => v.trim())
    .filter((v) => v !== '')
}

/** The values of the fixed list among the given ones, in the list's order, each once; unknown values are dropped. */
function known<T extends string>(list: readonly T[], given: readonly string[]): T[] {
  return list.filter((value) => given.includes(value))
}

function flag(params: URLSearchParams, name: string): boolean {
  return params.get(name) === '1'
}

/**
 * Reads the filters from a query string. Anything malformed is ignored rather than refused: an edited or outdated link
 * still opens the map, with only the conditions it names correctly.
 */
export function parseFilters(params: URLSearchParams): Filters {
  const by: string[] = []
  for (const raw of params.getAll(MapParamBy)) {
    const name = raw.trim()
    if (name !== '' && name.length <= maxSignatureLength && !by.includes(name)) {
      by.push(name)
    }
  }
  return {
    slots: known(timeSlots, listParam(params, MapParamSlots)),
    categories: known(categories, listParam(params, MapParamCategory)),
    cuisines: known(cuisines, listParam(params, MapParamCuisine)),
    by,
    mustTry: flag(params, MapParamMustTry),
    noAvoid: flag(params, MapParamNoAvoid),
  }
}

/**
 * Writes the filters into params, replacing whatever filters it held and keeping every other parameter: the fixed
 * values comma-separated in their lists' order, each signature as a parameter of its own, the two switches as "1" when on. Returns params.
 */
export function writeFilters(params: URLSearchParams, filters: Filters): URLSearchParams {
  for (const name of filterParams) {
    params.delete(name)
  }
  const lists: [string, readonly string[]][] = [
    [MapParamSlots, known(timeSlots, filters.slots)],
    [MapParamCategory, known(categories, filters.categories)],
    [MapParamCuisine, known(cuisines, filters.cuisines)],
  ]
  for (const [name, values] of lists) {
    if (values.length > 0) {
      params.set(name, values.join(','))
    }
  }
  for (const name of filters.by) {
    params.append(MapParamBy, name)
  }
  if (filters.mustTry) {
    params.set(MapParamMustTry, '1')
  }
  if (filters.noAvoid) {
    params.set(MapParamNoAvoid, '1')
  }
  return params
}

/** How many conditions are chosen: every value of the lists, and each switch that is on. */
export function filterCount(filters: Filters): number {
  return (
    filters.slots.length +
    filters.categories.length +
    filters.cuisines.length +
    filters.by.length +
    (filters.mustTry ? 1 : 0) +
    (filters.noAvoid ? 1 : 0)
  )
}

/** A stable string of the filters, for query keys: the same filters give the same string. */
export function filtersKey(filters: Filters): string {
  return writeFilters(new URLSearchParams(), filters).toString()
}

/**
 * The query string the map and the list share, from the current one: the city and the filters, nothing else (no
 * recommendation of a share link, no parameter of another page). Starts with "?" unless empty.
 */
export function mapSearch(params: URLSearchParams): string {
  const kept = new URLSearchParams()
  const city = params.get(MapParamCity)
  if (city) {
    kept.set(MapParamCity, city)
  }
  writeFilters(kept, parseFilters(params))
  const s = kept.toString()
  return s === '' ? '' : `?${s}`
}

/** Adds value to the list, or removes it when it is there already. */
export function toggled<T>(list: readonly T[], value: T): T[] {
  return list.includes(value) ? list.filter((v) => v !== value) : [...list, value]
}
