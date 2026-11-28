import { useQuery } from '@tanstack/react-query'
import type { Map as MapLibreMap } from 'maplibre-gl'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useFilters } from '@/filters/useFilters'
import { instanceQuery } from '@/instance/instance'
import type { Bounds } from '../cities'
import { clipBox, containsBox, fetchBox } from './geo'
import { placesQuery } from './placesApi'
import { rememberView } from './view'

/** How long the map must rest after a move before it asks for the places of a new area. */
export const moveDebounceMillis = 300

/** The bounds of what the map shows. */
function viewOf(map: MapLibreMap): Bounds {
  const b = map.getBounds()
  return [b.getWest(), b.getSouth(), b.getEast(), b.getNorth()]
}

/**
 * The places the map shows (docs/architecture.md §4.3): those of the city around the current view that pass the
 * address's filters. Call onView with the map whenever it has loaded or finished a move: it remembers the view (for
 * the list, view.ts) and, once the map has rested for a moment, asks for a larger area when the view left the one
 * loaded. Only cities of the instance settings have places; an archive the settings do not name has none.
 */
export function useMapPlaces(city: string, cityBounds: Bounds) {
  const [filters] = useFilters()
  const instance = useQuery(instanceQuery)
  const known = instance.data?.cities.some((c) => c.slug === city) ?? false
  const [area, setArea] = useState<{ city: string; box: Bounds } | null>(null)
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
  // The city whose places were asked for last.
  const shown = useRef<string | null>(null)

  useEffect(
    () => () => {
      clearTimeout(timer.current)
    },
    [],
  )

  const onView = useCallback(
    (map: MapLibreMap) => {
      const view = viewOf(map)
      const center = map.getCenter()
      rememberView({ city, bounds: view, center: [center.lng, center.lat], zoom: map.getZoom() })
      const update = () => {
        const visible = clipBox(view, cityBounds) ?? cityBounds
        setArea((prev) =>
          prev?.city === city && containsBox(prev.box, visible) ? prev : { city, box: fetchBox(view, cityBounds) },
        )
      }
      clearTimeout(timer.current)
      // The first view of a city is loaded at once; later moves once the map rests.
      if (shown.current === city) {
        timer.current = setTimeout(update, moveDebounceMillis)
      } else {
        shown.current = city
        update()
      }
    },
    [city, cityBounds],
  )

  const box = area?.city === city ? area.box : null
  const query = useQuery({ ...placesQuery(city, box, filters), enabled: known && box !== null })
  return { query, onView, filters, known }
}
