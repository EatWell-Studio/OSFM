// The instance settings members see, GET /api/osfm/instance (settings.Instance on the server): the instance's name
// and its cities, and whether place search is on. A city's slug is also the name of its basemap archive (/tiles/<slug>/…), which may not exist yet.

import { queryOptions } from '@tanstack/react-query'
import { apiJson } from '@/lib/api'

export interface InstanceCity {
  slug: string
  name: string
  /** minLon, minLat, maxLon, maxLat */
  bbox: [number, number, number, number]
  /** lon, lat */
  center: [number, number]
}

export interface Instance {
  name: string
  cities: InstanceCity[]
  /**
   * Whether place search is on (the search box is hidden when it is false); absent from responses that do not say,
   * such as the setup's.
   */
  geocoderEnabled?: boolean
}

export const instancePath = '/instance'
export const instanceQueryKey = ['instance'] as const

const slugPattern = /^[a-z0-9-]{1,64}$/

function isNumbers(value: unknown, length: number): boolean {
  return Array.isArray(value) && value.length === length && value.every((n) => typeof n === 'number' && isFinite(n))
}

export function isInstanceCity(value: unknown): value is InstanceCity {
  if (typeof value !== 'object' || value === null) {
    return false
  }
  const c = value as Record<string, unknown>
  return (
    typeof c.slug === 'string' &&
    slugPattern.test(c.slug) &&
    typeof c.name === 'string' &&
    c.name !== '' &&
    isNumbers(c.bbox, 4) &&
    isNumbers(c.center, 2)
  )
}

/** Reads the body of GET /api/osfm/instance; malformed cities are left out. */
export function parseInstance(body: unknown): Instance {
  if (typeof body !== 'object' || body === null) {
    throw new Error(`${instancePath}: unexpected response`)
  }
  const b = body as Record<string, unknown>
  const instance: Instance = {
    name: typeof b.name === 'string' ? b.name : '',
    cities: Array.isArray(b.cities) ? b.cities.filter(isInstanceCity) : [],
  }
  if (typeof b.geocoderEnabled === 'boolean') {
    instance.geocoderEnabled = b.geocoderEnabled
  }
  return instance
}

export const instanceQuery = queryOptions({
  queryKey: instanceQueryKey,
  queryFn: async () => parseInstance(await apiJson(instancePath)),
  staleTime: 5 * 60 * 1000,
})
