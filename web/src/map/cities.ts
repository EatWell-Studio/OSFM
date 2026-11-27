// The cities the map can show (docs/architecture.md §3.6, §4.3): those of the instance settings, by their display
// names, and every basemap archive on the server, listed by GET /tiles/ with the bounds, center and zoom levels of its
// TileJSON. A city of the settings without an archive has no tiles yet.

import type { InstanceCity } from '@/instance/instance'
import { ApiError } from '@/lib/api'

/** minLon, minLat, maxLon, maxLat in WGS-84 degrees. */
export type Bounds = [number, number, number, number]

export interface City {
  /** The name the server knows the city by, as in /tiles/<id>/…: lowercase letters, digits and '-'. */
  id: string
  bounds: Bounds
  /** lon, lat, zoom */
  center: [number, number, number]
  minzoom: number
  maxzoom: number
}

export const citiesUrl = '/tiles/'
export const citiesQueryKey = ['tiles', 'cities'] as const

const idPattern = /^[a-z0-9-]{1,64}$/

function isNumbers<T extends number[]>(value: unknown, length: T['length']): value is T {
  return Array.isArray(value) && value.length === length && value.every((n) => typeof n === 'number' && isFinite(n))
}

function isCity(value: unknown): value is City {
  if (typeof value !== 'object' || value === null) {
    return false
  }
  const c = value as Record<string, unknown>
  return (
    typeof c.id === 'string' &&
    idPattern.test(c.id) &&
    isNumbers<Bounds>(c.bounds, 4) &&
    c.bounds[0] < c.bounds[2] &&
    c.bounds[1] < c.bounds[3] &&
    isNumbers<City['center']>(c.center, 3) &&
    typeof c.minzoom === 'number' &&
    typeof c.maxzoom === 'number'
  )
}

/** Picks the valid cities out of the body of GET /tiles/; anything malformed is left out. */
export function parseCities(body: unknown): City[] {
  if (typeof body !== 'object' || body === null || !Array.isArray((body as { cities?: unknown }).cities)) {
    throw new Error(`${citiesUrl}: not a city index`)
  }
  return (body as { cities: unknown[] }).cities.filter(isCity)
}

/**
 * Fetches the city index. Same origin with the session cookie, like every request of the app: the tiles require a
 * signed-in member. A 401 is an ApiError, so that the query client re-checks the session (and so sign-in).
 */
export async function fetchCities(signal?: AbortSignal): Promise<City[]> {
  const response = await fetch(citiesUrl, {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
    signal,
  })
  if (!response.ok) {
    throw new ApiError(response.status, citiesUrl)
  }
  return parseCities(await response.json())
}

/**
 * The name an archive without a city in the instance settings is shown by: its id with each word capitalised
 * ("example-town" → "Example Town").
 */
export function cityLabel(city: { id: string }): string {
  return city.id
    .split('-')
    .filter((word) => word !== '')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ')
}

/** Whether lon/lat lies within the bounds. */
export function contains(bounds: Bounds, lon: number, lat: number): boolean {
  return lon >= bounds[0] && lon <= bounds[2] && lat >= bounds[1] && lat <= bounds[3]
}

/** A city the map offers: its id (the slug), the name it is shown by, and its tiles, unless it has none yet. */
export interface MapCity {
  id: string
  label: string
  tiles?: City
}

/**
 * The cities to offer: those of the instance settings in their order, with their display names, then the archives
 * that no city of the settings names, with names made from their ids.
 */
export function mergeCities(archives: City[], configured: InstanceCity[]): MapCity[] {
  const byId = new Map(archives.map((a) => [a.id, a]))
  const result: MapCity[] = configured.map((c) => ({ id: c.slug, label: c.name, tiles: byId.get(c.slug) }))
  const named = new Set(configured.map((c) => c.slug))
  for (const a of archives) {
    if (!named.has(a.id)) {
      result.push({ id: a.id, label: cityLabel(a), tiles: a })
    }
  }
  return result
}

/** The city to show: the requested one if it exists, else the remembered one, else the first. */
export function pickCity<T extends { id: string }>(
  cities: T[],
  requested: string | null,
  remembered: string | null,
): T | undefined {
  return cities.find((c) => c.id === requested) ?? cities.find((c) => c.id === remembered) ?? cities[0]
}
