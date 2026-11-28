// A new place's values, checks and request (B2–B5): the place's fields of /new around its first recommendation. Only
// the name is required of the place (B2, M2-Q3); the point comes from the crosshair or a search result (B1, B7, B12).

import type { CreatePlaceRequest, LandmarkInput } from '@/api/types.gen'
import type { Category, Cuisine, Precision } from '@/lib/enums'
import { cuisines } from '@/lib/enums'
import {
  limits,
  lineProblem,
  type Problems,
  recommendationProblems,
  type RecommendationValues,
  textProblem,
  toRecommendationInput,
} from './recommendationForm'

/**
 * The point of the new place. source is "search" while it is the place search result the member picked and has not
 * moved from, "crosshair" otherwise (B12). Never shown as numbers (H3).
 */
export interface Point {
  lat: number
  lon: number
  source: 'crosshair' | 'search'
}

/** What the place's position is described relative to (B4): nothing, a place nearby, or a text. */
export type LandmarkValues = { type: 'none' } | { type: 'place'; place: string } | { type: 'text'; text: string }

export interface PlaceValues {
  name: string
  precision: Precision
  /** "" for none (M2-Q5: the category is optional) */
  category: Category | ''
  cuisine: Cuisine[]
  positionNote: string
  landmark: LandmarkValues
}

export const emptyPlace: PlaceValues = {
  name: '',
  precision: 'exact',
  category: '',
  cuisine: [],
  positionNote: '',
  landmark: { type: 'none' },
}

/** The problems of the place and its first recommendation, none when they can be sent. */
export function newPlaceProblems(
  place: PlaceValues,
  recommendation: RecommendationValues,
  options: { canUsePseudonym: boolean; today?: string },
): Problems {
  const problems: Problems = {}
  const name = lineProblem(place.name, limits.name, true)
  if (name) {
    problems.name = name
  }
  const note = textProblem(place.positionNote, limits.positionNote, false)
  if (note) {
    problems.positionNote = note
  }
  if (place.landmark.type === 'text') {
    const landmark = lineProblem(place.landmark.text, limits.landmark, true)
    if (landmark) {
      problems.landmark = landmark
    }
  } else if (place.landmark.type === 'place' && place.landmark.place === '') {
    problems.landmark = 'required'
  }
  return { ...problems, ...recommendationProblems(recommendation, options) }
}

/** The request for a new place at point, whose values newPlaceProblems found nothing wrong with. */
export function toCreatePlaceRequest(
  place: PlaceValues,
  recommendation: RecommendationValues,
  point: Point,
): CreatePlaceRequest {
  const request: CreatePlaceRequest = {
    name: place.name.trim(),
    lat: point.lat,
    lon: point.lon,
    sourceCoord: { crs: 'wgs84', source: point.source },
    recommendation: toRecommendationInput(recommendation),
  }
  if (place.precision !== 'exact') {
    request.precision = place.precision
  }
  if (place.category !== '') {
    request.category = place.category
  }
  if (place.cuisine.length > 0) {
    request.cuisine = cuisines.filter((c) => place.cuisine.includes(c))
  }
  if (place.positionNote.trim() !== '') {
    request.positionNote = place.positionNote.trim()
  }
  let landmark: LandmarkInput | null = null
  if (place.landmark.type === 'place') {
    landmark = { type: 'place', place: place.landmark.place }
  } else if (place.landmark.type === 'text' && place.landmark.text.trim() !== '') {
    landmark = { type: 'text', text: place.landmark.text.trim() }
  }
  if (landmark) {
    request.landmark = landmark
  }
  return request
}
