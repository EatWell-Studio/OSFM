import { distanceMetres } from '@/map/places/geo'
import type { Place } from '@/map/places/placesApi'

export interface PlaceAtDistance {
  place: Place
  /** In metres, unrounded: rounded only for display (H3). */
  distance: number
}

/**
 * The places by distance from origin, the nearest first; places at the same distance by name. Distance is the only
 * order of the list: nothing is ranked by attitude (C11).
 */
export function byDistance(places: readonly Place[], origin: { lon: number; lat: number }): PlaceAtDistance[] {
  return places
    .map((place) => ({ place, distance: distanceMetres(origin, place) }))
    .sort(
      (a, b) =>
        a.distance - b.distance || a.place.name.localeCompare(b.place.name) || a.place.uuid.localeCompare(b.place.uuid),
    )
}
