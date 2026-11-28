// Geometry of the map's places (docs/architecture.md §4.3): circle sizes in metres, the boxes the map asks for, and
// distances for the list. Pure functions, no map library.

import type { Precision } from '@/lib/enums'
import type { Bounds } from '../cities'

/** The radius, in metres, a place of each precision is drawn with (B3): a pin for exact, circles for the others. */
export const precisionRadius: Record<Exclude<Precision, 'exact'>, number> = { approx: 50, area: 300 }

/** The circumference of the earth at the equator in Web Mercator, in metres. */
const earthCircumference = 40_075_016.686

/** MapLibre's world is 512 CSS pixels wide at zoom 0. */
const worldSize = 512

/** Metres per CSS pixel at the latitude (degrees) and zoom level. */
export function metresPerPixel(lat: number, zoom: number): number {
  return (earthCircumference * Math.cos((lat * Math.PI) / 180)) / (worldSize * 2 ** zoom)
}

/**
 * The radius in CSS pixels at zoom 0 of a circle of the given metres at the latitude: the radius at zoom z is this
 * times 2^z, so that the circle keeps its size on the ground (the style interpolates it per zoom level).
 */
export function radiusAtZoom0(metres: number, lat: number): number {
  return metres / metresPerPixel(lat, 0)
}

/** Whether inner lies entirely within outer. */
export function containsBox(outer: Bounds, inner: Bounds): boolean {
  return inner[0] >= outer[0] && inner[1] >= outer[1] && inner[2] <= outer[2] && inner[3] <= outer[3]
}

/** The part of box within limit, or null when they do not overlap. */
export function clipBox(box: Bounds, limit: Bounds): Bounds | null {
  const clipped: Bounds = [
    Math.max(box[0], limit[0]),
    Math.max(box[1], limit[1]),
    Math.min(box[2], limit[2]),
    Math.min(box[3], limit[3]),
  ]
  return clipped[0] < clipped[2] && clipped[1] < clipped[3] ? clipped : null
}

/**
 * The box the map asks the server for, around the view: the view grown by margin times its size on each side, rounded
 * outwards to 0.001° (about 100 m, so that small moves give the same box and the same query) and clipped to the city.
 * While the view stays within it, moving the map asks for nothing new.
 */
export function fetchBox(view: Bounds, city: Bounds, margin = 0.5): Bounds {
  const dx = (view[2] - view[0]) * margin
  const dy = (view[3] - view[1]) * margin
  const step = 1000
  const grown: Bounds = [
    Math.floor((view[0] - dx) * step) / step,
    Math.floor((view[1] - dy) * step) / step,
    Math.ceil((view[2] + dx) * step) / step,
    Math.ceil((view[3] + dy) * step) / step,
  ]
  return clipBox(grown, city) ?? city
}

/** The distance in metres between two points (lon, lat in degrees), on a sphere: good to well under 1 % in a city. */
export function distanceMetres(a: { lon: number; lat: number }, b: { lon: number; lat: number }): number {
  const rad = Math.PI / 180
  const dLat = (b.lat - a.lat) * rad
  const dLon = (b.lon - a.lon) * rad
  const h = Math.sin(dLat / 2) ** 2 + Math.cos(a.lat * rad) * Math.cos(b.lat * rad) * Math.sin(dLon / 2) ** 2
  return 2 * 6_371_008.8 * Math.asin(Math.min(1, Math.sqrt(h)))
}

/**
 * A distance as the list shows it (H3: nothing finer than about 10 m): under 1 km in metres rounded to 10 m, from 1 km
 * in kilometres rounded to 0.1 km.
 */
export function roundedDistance(metres: number): { unit: 'm' | 'km'; value: number } {
  const tens = Math.round(metres / 10) * 10
  if (tens < 1000) {
    return { unit: 'm', value: tens }
  }
  return { unit: 'km', value: Math.round(metres / 100) / 10 }
}
