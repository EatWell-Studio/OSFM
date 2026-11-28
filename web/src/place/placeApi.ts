// A place's card: GET /api/osfm/places/{uuid} (internal/api/map.go). The server decides everything the card shows: which
// recommendations the member may see, their signatures, the counts (numbers only, never an average) and whether a
// hidden recommendation is listed. A place the member may not see answers 404, like one that does not exist.
//
// Features that change a place or its recommendations invalidate placeCardQueryKey(uuid) afterwards (no optimistic
// updates, docs/architecture.md §4.4).

import { queryOptions } from '@tanstack/react-query'
import { type PlaceCard, PlaceCardPath, type RecommendationView } from '@/api/types.gen'
import { ApiError, apiJson } from '@/lib/api'
import { field } from '@/lib/authApi'

export type { PlaceCard, RecommendationView }

/** The card's path relative to the API base (apiJson takes paths under /api/osfm). */
export function placeCardPath(place: string): string {
  return PlaceCardPath.replace(/^\/api\/osfm/, '').replace('{uuid}', encodeURIComponent(place))
}

export const placeCardQueryKey = (place: string) => ['place', place, 'card'] as const

const isString = (value: unknown): value is string => typeof value === 'string'
const isStrings = (value: unknown): value is string[] => Array.isArray(value) && value.every(isString)
const isCount = (value: unknown): value is number => typeof value === 'number' && Number.isInteger(value) && value >= 0

function isCounts(value: unknown): boolean {
  return ['mustTry', 'good', 'ok', 'avoid'].every((name) => isCount(field(value, name)))
}

function isRecommendation(value: unknown): value is RecommendationView {
  return (
    isString(field(value, 'uuid')) &&
    isString(field(value, 'displayName')) &&
    isString(field(value, 'attitude')) &&
    isString(field(value, 'text')) &&
    isStrings(field(value, 'dishes')) &&
    isStrings(field(value, 'timeSlots')) &&
    Array.isArray(field(value, 'sources')) &&
    isString(field(value, 'createdAt'))
  )
}

/** Reads a card from a response body; throws when it does not have the shape of one. */
export function parsePlaceCard(body: unknown): PlaceCard {
  const recommendations = field(body, 'recommendations')
  if (
    !isString(field(body, 'uuid')) ||
    !isString(field(body, 'name')) ||
    !isStrings(field(body, 'aliases')) ||
    typeof field(body, 'lat') !== 'number' ||
    typeof field(body, 'lon') !== 'number' ||
    !isString(field(body, 'precision')) ||
    !isString(field(body, 'positionNote')) ||
    !isString(field(body, 'category')) ||
    !isStrings(field(body, 'cuisine')) ||
    !isString(field(body, 'city')) ||
    !isCounts(field(body, 'counts')) ||
    !Array.isArray(recommendations) ||
    !recommendations.every(isRecommendation)
  ) {
    throw new Error(`${PlaceCardPath}: unexpected response`)
  }
  return body as PlaceCard
}

/** Whether the error says the place is not there for this member (missing, deleted or not visible: the same 404). */
export function isPlaceNotFound(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404
}

/** The card of a place. A 4xx answer is final (no retries): a missing place is reported at once. */
export const placeCardQuery = (place: string) =>
  queryOptions({
    queryKey: placeCardQueryKey(place),
    queryFn: async ({ signal }) => parsePlaceCard(await apiJson(placeCardPath(place), { signal })),
    staleTime: 0,
    retry: (failures, error) => !(error instanceof ApiError && error.status < 500) && failures < 2,
  })
