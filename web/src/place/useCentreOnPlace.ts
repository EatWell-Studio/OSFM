// Centring the map on the place whose card is open (A7). The card is a child route of the map page (/p/:place), so the
// map reads the place from the route and its position from the card's own query (the same cache entry, no second
// request). Switching to the place's city is the card's part: it sets the map's ?city= (PlacePage), and once the map
// shows that city, this centres it.

import { useQuery } from '@tanstack/react-query'
import type { MapRef } from '@vis.gl/react-maplibre'
import { type RefObject, useCallback, useEffect, useRef } from 'react'
import { useMatch } from 'react-router'
import { type Bounds, contains } from '@/map/cities'
import { isUuid, placePath } from '@/lib/paths'
import { placeCardQuery } from './placeApi'

/** The zoom a place is shown at, at least: street level. */
export const placeZoom = 15

/** The place of the current route, /p/:place, when it is a well-formed UUID; null on every other page. */
export function useOpenPlace(): string | null {
  const match = useMatch(placePath(':place'))
  const place = match?.params.place
  return isUuid(place) ? place : null
}

/**
 * Centres the map on the open place once per opening, when the place lies in the city shown (bounds). Returns a
 * function for the map's load event: before the map is there, nothing can be centred yet.
 */
export function useCentreOnPlace(mapRef: RefObject<MapRef | null>, bounds: Bounds): () => void {
  const place = useOpenPlace()
  const card = useQuery({ ...placeCardQuery(place ?? ''), enabled: place !== null })
  const target = place !== null && card.data?.uuid === place ? card.data : null
  const lat = target?.lat
  const lon = target?.lon
  // The place last centred, so that refetching the card (or zooming out afterwards) does not move the map again.
  const centred = useRef<string | null>(null)

  const centre = useCallback(() => {
    if (place === null) {
      centred.current = null
      return
    }
    const map = mapRef.current
    if (!map || lat === undefined || lon === undefined || centred.current === place || !contains(bounds, lon, lat)) {
      return
    }
    centred.current = place
    map.easeTo({ center: [lon, lat], zoom: Math.max(map.getZoom(), placeZoom) })
  }, [mapRef, bounds, place, lat, lon])

  useEffect(centre, [centre])
  return centre
}
