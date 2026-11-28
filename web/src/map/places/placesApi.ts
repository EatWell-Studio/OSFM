// The places of the map and the list, GET /api/osfm/places (internal/api/map.go; docs/architecture.md §3.2, §4.3), and
// the signatures to filter by, GET /api/osfm/signatures. Both answer with Cache-Control: private, no-cache and an ETag:
// the browser asks again every time and an unchanged answer costs a 304.
//
// No map library here: the list uses this module too, without loading MapLibre.

import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import {
  type MapPlace,
  MapParamBBox,
  MapParamCity,
  MapPlacesPath,
  type PlaceCounts,
  SignaturesPath,
} from '@/api/types.gen'
import { type Filters, filtersKey, writeFilters } from '@/filters/filters'
import { apiBase, apiJson } from '@/lib/api'
import { type Precision, precisions } from '@/lib/enums'
import type { Bounds } from '../cities'

export type { MapPlace }

/** A place as the app uses it: precision is one of the three (anything else is drawn as the vaguest, an area). */
export interface Place extends Omit<MapPlace, 'precision'> {
  precision: Precision
}

/** The generated paths are absolute; apiJson takes them relative to /api/osfm. */
function relative(path: string): string {
  return path.startsWith(apiBase) ? path.slice(apiBase.length) : path
}

function isCount(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0
}

function isCounts(value: unknown): value is PlaceCounts {
  if (typeof value !== 'object' || value === null) {
    return false
  }
  const c = value as Record<string, unknown>
  return isCount(c.mustTry) && isCount(c.good) && isCount(c.ok) && isCount(c.avoid)
}

function toPlace(value: unknown): Place | null {
  if (typeof value !== 'object' || value === null) {
    return null
  }
  const p = value as Record<string, unknown>
  if (
    typeof p.uuid !== 'string' ||
    typeof p.name !== 'string' ||
    typeof p.lat !== 'number' ||
    typeof p.lon !== 'number' ||
    !isFinite(p.lat) ||
    !isFinite(p.lon) ||
    !isCounts(p.counts)
  ) {
    return null
  }
  const precision = precisions.find((v) => v === p.precision) ?? 'area'
  return {
    uuid: p.uuid,
    name: p.name,
    lat: p.lat,
    lon: p.lon,
    precision,
    category: typeof p.category === 'string' ? p.category : '',
    counts: p.counts,
    timeSlots: Array.isArray(p.timeSlots) ? p.timeSlots.filter((s): s is string => typeof s === 'string') : [],
  }
}

/** Reads the body of GET /api/osfm/places; malformed entries are left out. */
export function parsePlaces(body: unknown): Place[] {
  if (!Array.isArray(body)) {
    throw new Error(`${MapPlacesPath}: unexpected response`)
  }
  return body.map(toPlace).filter((p): p is Place => p !== null)
}

/** A box as the bbox parameter: minLon,minLat,maxLon,maxLat with at most 5 decimals (about 1 m). */
export function bboxParam(box: Bounds): string {
  return box.map((n) => String(Math.round(n * 1e5) / 1e5)).join(',')
}

/** The places of the city within box (the whole city without one) that pass the filters. */
export async function fetchPlaces(
  city: string,
  box: Bounds | null,
  filters: Filters,
  signal?: AbortSignal,
): Promise<Place[]> {
  const params = new URLSearchParams({ [MapParamCity]: city })
  if (box) {
    params.set(MapParamBBox, bboxParam(box))
  }
  writeFilters(params, filters)
  return parsePlaces(await apiJson(`${relative(MapPlacesPath)}?${params.toString()}`, { signal }))
}

/**
 * The query of the places: keyed by city, box and filters. While a new box or new filters load, the places shown
 * before stay (no flicker on every move of the map).
 */
export function placesQuery(city: string, box: Bounds | null, filters: Filters) {
  return queryOptions({
    queryKey: ['places', city, box ? bboxParam(box) : 'city', filtersKey(filters)] as const,
    queryFn: ({ signal }) => fetchPlaces(city, box, filters, signal),
    placeholderData: keepPreviousData,
  })
}

/** Reads the body of GET /api/osfm/signatures: the names, each once, in the server's order. */
export function parseSignatures(body: unknown): string[] {
  if (!Array.isArray(body)) {
    throw new Error(`${SignaturesPath}: unexpected response`)
  }
  return [...new Set(body.filter((n): n is string => typeof n === 'string' && n !== ''))]
}

/** The signatures of the recommendations the member can see in the city, to filter by. */
export function signaturesQuery(city: string) {
  return queryOptions({
    queryKey: ['signatures', city] as const,
    queryFn: async ({ signal }) =>
      parseSignatures(
        await apiJson(`${relative(SignaturesPath)}?${new URLSearchParams({ [MapParamCity]: city }).toString()}`, {
          signal,
        }),
      ),
  })
}
