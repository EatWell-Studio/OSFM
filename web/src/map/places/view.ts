// The map's last view: which part of which city it showed. The list shows the places of this view, by distance from
// its center (A6), and the map opens on it again when the member comes back from the list or a card. Kept in memory
// and in the tab's session storage (a reload of /list keeps it), never sent anywhere.

import type { Bounds } from '../cities'

export interface MapViewState {
  city: string
  /** The visible area. */
  bounds: Bounds
  /** lon, lat */
  center: [number, number]
  zoom: number
}

const storageKey = 'osfm.map.view'

let current: MapViewState | null = null

function isNumbers(value: unknown, length: number): value is number[] {
  return Array.isArray(value) && value.length === length && value.every((n) => typeof n === 'number' && isFinite(n))
}

function parseView(value: unknown): MapViewState | null {
  if (typeof value !== 'object' || value === null) {
    return null
  }
  const v = value as Record<string, unknown>
  if (typeof v.city !== 'string' || !isNumbers(v.bounds, 4) || !isNumbers(v.center, 2) || typeof v.zoom !== 'number') {
    return null
  }
  return { city: v.city, bounds: v.bounds as Bounds, center: v.center as [number, number], zoom: v.zoom }
}

function stored(): MapViewState | null {
  try {
    const raw = sessionStorage.getItem(storageKey)
    return raw ? parseView(JSON.parse(raw)) : null
  } catch {
    return null
  }
}

/** The last view of the city, if the map showed it last. */
export function lastView(city: string): MapViewState | null {
  current ??= stored()
  return current?.city === city ? current : null
}

export function rememberView(view: MapViewState) {
  current = view
  try {
    sessionStorage.setItem(storageKey, JSON.stringify(view))
  } catch {
    // A convenience only.
  }
}
