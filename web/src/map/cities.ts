// The cities the map can show: one per basemap archive on the server, listed by GET /tiles/ (docs/architecture.md
// §3.6, §4.3). Each comes with the bounds, center and zoom levels of its TileJSON.

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
 * The name a city is shown by. The instance settings will give cities their display names (setup wizard); until then
 * it is the city's id with each word capitalised ("example-town" → "Example Town").
 */
export function cityLabel(city: City): string {
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

/** The city to show: the requested one if it exists, else the remembered one, else the first. */
export function pickCity(cities: City[], requested: string | null, remembered: string | null): City | undefined {
  return cities.find((c) => c.id === requested) ?? cities.find((c) => c.id === remembered) ?? cities[0]
}
