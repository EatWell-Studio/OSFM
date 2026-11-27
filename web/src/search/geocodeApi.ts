// Place search, GET /api/osfm/geocode (internal/api/geocode.go; docs/architecture.md §3.7): the instance asks the
// search services for the browser, which talks to no one else. One request per submitted search, never as the member
// types (the services' usage policies forbid search as you type).

import type { GeocodeResult } from '@/api/types.gen'
import { ApiError, apiFetch, isJson, retryAfterSeconds } from '@/lib/api'

export type { GeocodeResult }

export const geocodePath = '/geocode'

/** The longest search term the server takes, in characters (api.MaxGeocodeQuery). */
export const maxQueryLength = 200

/** Why a search found nothing to show. */
export type SearchFailure =
  /** The maintainers turned place search off. */
  | { kind: 'disabled' }
  /** Too many searches: try again after retryAfter seconds (null: unknown). */
  | { kind: 'rateLimited'; retryAfter: number | null }
  /** The search services, the instance or the network did not answer. */
  | { kind: 'unavailable' }

/** A failed search; failure says why. */
export class SearchError extends Error {
  readonly failure: SearchFailure

  constructor(failure: SearchFailure) {
    super(`place search: ${failure.kind}`)
    this.name = 'SearchError'
    this.failure = failure
  }
}

function isResult(value: unknown): value is GeocodeResult {
  if (typeof value !== 'object' || value === null) {
    return false
  }
  const r = value as Record<string, unknown>
  return (
    typeof r.name === 'string' &&
    r.name !== '' &&
    typeof r.label === 'string' &&
    typeof r.lat === 'number' &&
    typeof r.lon === 'number' &&
    isFinite(r.lat) &&
    isFinite(r.lon)
  )
}

/** Reads the body of a successful search; malformed entries are left out. */
export function parseResults(body: unknown): GeocodeResult[] {
  if (!Array.isArray(body)) {
    throw new Error(`${geocodePath}: unexpected response`)
  }
  return body.filter(isResult)
}

async function errorCode(response: Response): Promise<string | null> {
  if (!isJson(response)) {
    return null
  }
  try {
    const code = ((await response.json()) as { code?: unknown } | null)?.code
    return typeof code === 'string' ? code : null
  } catch {
    return null
  }
}

/**
 * Searches for places named query in the city (its slug). Throws a SearchError when the search cannot be done, or an
 * AbortError when signal aborts.
 */
export async function searchPlaces(query: string, city: string, signal?: AbortSignal): Promise<GeocodeResult[]> {
  const params = new URLSearchParams({ q: query, city })
  let response: Response
  try {
    response = await apiFetch(`${geocodePath}?${params.toString()}`, { signal })
  } catch (e) {
    if (signal?.aborted) {
      throw e
    }
    throw new SearchError({ kind: 'unavailable' })
  }
  if (response.ok) {
    try {
      return parseResults(await response.json())
    } catch {
      throw new SearchError({ kind: 'unavailable' })
    }
  }
  const code = await errorCode(response)
  if (response.status === 404 && code === 'geocoder_disabled') {
    throw new SearchError({ kind: 'disabled' })
  }
  if (response.status === 429) {
    throw new SearchError({ kind: 'rateLimited', retryAfter: retryAfterSeconds(response) })
  }
  if (response.status === 401) {
    // The session ended: the app's session handling takes over, as for any other request.
    throw new ApiError(response.status, geocodePath)
  }
  throw new SearchError({ kind: 'unavailable' })
}
