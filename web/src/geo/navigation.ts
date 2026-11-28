// Links that open a place in a navigation app (A5, docs/architecture.md §4.6). They are plain links: the app follows one
// only when the member taps it, as a top-level navigation in a new window (on phones the system hands it to the app);
// the page itself never requests anything from these sites (A1). Their bases are in the allow-list of
// scripts/check-external-urls.ts.
//
// The database holds WGS-84. Each app expects its own datum in mainland China (./mainland.ts decides whether a place is
// there); the table below is the result of checking the generated links, written up in §4.6:
//   - Amap draws GCJ-02 and reads GCJ-02 by default. In mainland China the link carries our GCJ-02 (coordinate=gaode);
//     elsewhere it passes WGS-84 and says so (coordinate=wgs84), so that Amap applies its own rules there.
//   - Google Maps draws its road map of mainland China in GCJ-02 and puts a pin at the coordinates as given, so a WGS-84
//     pin lands some hundred metres off: the link carries GCJ-02 there.
//   - Apple Maps reads the coordinates of its links as WGS-84 and converts them itself: the link carries WGS-84
//     everywhere.
// Outside mainland China every app gets WGS-84. Offsets on devices are measured in T021.

import { type LatLon, wgs84ToGcj02 } from './gcj02'
import { inMainlandChina } from './mainland'

export const navigationApps = ['amap', 'apple', 'google'] as const
export type NavigationApp = (typeof navigationApps)[number]

/** The datum each app expects for a place in mainland China. */
export const mainlandDatum: Record<NavigationApp, 'gcj02' | 'wgs84'> = {
  amap: 'gcj02',
  apple: 'wgs84',
  google: 'gcj02',
}

/** What a link needs of a place: its WGS-84 position and its name (a label for the pin where the app takes one). */
export interface NavigationTarget extends LatLon {
  name: string
}

// The bases, as complete strings, so that the build's allow-list matches them exactly.
const amapMarker = 'https://uri.amap.com/marker'
const appleMaps = 'https://maps.apple.com/'
const googleSearch = 'https://www.google.com/maps/search/'

/** Degrees with six decimals (about 0.1 m), without trailing zeros. */
function degrees(value: number): string {
  return String(Number(value.toFixed(6)))
}

/** The link that opens the place in the app. */
export function navigationLink(app: NavigationApp, target: NavigationTarget): string {
  const mainland = inMainlandChina(target)
  const p = mainland && mainlandDatum[app] === 'gcj02' ? wgs84ToGcj02(target) : target
  const lat = degrees(p.lat)
  const lon = degrees(p.lon)
  const name = encodeURIComponent(target.name)
  switch (app) {
    case 'amap':
      // uri.amap.com/marker: a pin with the name; callnative=1 opens the app on phones where it is installed.
      return `${amapMarker}?position=${lon},${lat}&name=${name}&coordinate=${mainland ? 'gaode' : 'wgs84'}&callnative=1`
    case 'apple':
      // The URL scheme every iOS version understands: ll places the pin, q labels it.
      return `${appleMaps}?ll=${lat},${lon}&q=${name}`
    case 'google':
      // Maps URLs: a coordinate query puts a pin at exactly that point (a name would search Google's places instead).
      return `${googleSearch}?api=1&query=${lat}%2C${lon}`
  }
}
