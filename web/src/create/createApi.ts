// Writing places and recommendations (internal/api/places.go): POST /api/osfm/places (a new place with its first
// recommendation), POST /api/osfm/places/{uuid}/recommendations (one's recommendation of a place that exists) and
// GET /api/osfm/places/nearby (the duplicate hint, B6). After a write the pages invalidate the queries that show
// places (invalidateAfterWrite); nothing is updated optimistically (docs/architecture.md §4.4).

import type { QueryClient } from '@tanstack/react-query'
import {
  type CreatedPlace,
  type CreatedRecommendation,
  type CreatePlaceRequest,
  type NearbyPlace,
  PlaceRecommendationsPath,
  PlacesNearbyPath,
  PlacesPath,
  type RecommendationInput,
} from '@/api/types.gen'
import { ApiError, apiBase, apiFetch, apiJson, isJson, retryAfterSeconds } from '@/lib/api'
import { field } from '@/lib/authApi'
import { placeCardQueryKey } from '@/place/placeApi'

export type { CreatePlaceRequest, NearbyPlace, RecommendationInput }

/** The generated paths are absolute; apiFetch takes them relative to /api/osfm. */
function relative(path: string): string {
  return path.startsWith(apiBase) ? path.slice(apiBase.length) : path
}

/**
 * A refused or failed write. status 0 means the request never got an answer (offline, or the network failed). code
 * is the server's error code; field the request field an invalid_request is about ("recommendation.text"); mine the
 * uuid of the member's own recommendation for already_recommended.
 */
export class WriteError extends ApiError {
  readonly code: string | null
  readonly field: string | null
  readonly mine: string | null

  constructor(
    status: number,
    path: string,
    details: { code?: string | null; field?: string | null; mine?: string | null; retryAfter?: number | null } = {},
  ) {
    super(status, path, details.retryAfter ?? null)
    this.name = 'WriteError'
    this.code = details.code ?? null
    this.field = details.field ?? null
    this.mine = details.mine ?? null
  }
}

function stringField(body: unknown, name: string): string | null {
  const value = field(body, name)
  return typeof value === 'string' ? value : null
}

/** POSTs body as JSON and returns the parsed answer; throws WriteError when refused or unanswered. */
async function post(path: string, body: unknown): Promise<unknown> {
  let response: Response
  try {
    response = await apiFetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })
  } catch {
    throw new WriteError(0, path)
  }
  const parsed: unknown = isJson(response) ? await response.json().catch(() => null) : null
  if (!response.ok) {
    const data = field(parsed, 'data')
    throw new WriteError(response.status, path, {
      code: stringField(parsed, 'code'),
      field: stringField(data, 'field'),
      mine: stringField(data, 'recommendation'),
      retryAfter: retryAfterSeconds(response),
    })
  }
  return parsed
}

/** Creates a place with its first recommendation; returns the uuids of both. */
export async function createPlace(request: CreatePlaceRequest): Promise<CreatedPlace> {
  const body = await post(relative(PlacesPath), request)
  const place = stringField(body, 'place')
  const recommendation = stringField(body, 'recommendation')
  if (place === null || recommendation === null) {
    throw new Error(`${PlacesPath}: unexpected response`)
  }
  return { place, recommendation }
}

/** Adds the member's recommendation to a place; returns its uuid. 409 already_recommended names their own one. */
export async function addRecommendation(place: string, input: RecommendationInput): Promise<CreatedRecommendation> {
  const path = relative(PlaceRecommendationsPath).replace('{uuid}', encodeURIComponent(place))
  const recommendation = stringField(await post(path, input), 'recommendation')
  if (recommendation === null) {
    throw new Error(`${PlaceRecommendationsPath}: unexpected response`)
  }
  return { recommendation }
}

function isNearbyPlace(value: unknown): value is NearbyPlace {
  return (
    typeof field(value, 'uuid') === 'string' &&
    typeof field(value, 'name') === 'string' &&
    typeof field(value, 'category') === 'string' &&
    typeof field(value, 'distance') === 'number'
  )
}

/** Reads the body of GET /api/osfm/places/nearby; malformed entries are left out. */
export function parseNearby(body: unknown): NearbyPlace[] {
  const places = field(body, 'places')
  if (!Array.isArray(places)) {
    throw new Error(`${PlacesNearbyPath}: unexpected response`)
  }
  return places.filter(isNearbyPlace)
}

/**
 * The places the member can see within 100 m of the point whose name is like name: the nearest first, at most 5, each
 * with its distance rounded to 10 m and no coordinates. The point stays between the browser and the instance.
 */
export const nearbyQuery = (point: { lat: number; lon: number }, name: string) => ({
  queryKey: ['nearby', point.lat, point.lon, name] as const,
  queryFn: async ({ signal }: { signal: AbortSignal }) => {
    const params = new URLSearchParams({ lat: String(point.lat), lon: String(point.lon), name })
    return parseNearby(await apiJson(`${relative(PlacesNearbyPath)}?${params.toString()}`, { signal }))
  },
  staleTime: 30_000,
  retry: false,
})

/**
 * After a place or recommendation was saved: the map's and the list's places (T028's ['places', …]), the signatures to
 * filter by, the duplicate hints and the card of the place load again.
 */
export async function invalidateAfterWrite(queryClient: QueryClient, place: string) {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: ['places'] }),
    queryClient.invalidateQueries({ queryKey: ['signatures'] }),
    queryClient.invalidateQueries({ queryKey: ['nearby'] }),
    queryClient.invalidateQueries({ queryKey: placeCardQueryKey(place) }),
  ])
}
