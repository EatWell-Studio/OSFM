// The caching rules of the service worker (docs/architecture.md §4.5), as plain functions that the service worker
// (sw.ts) and the page (src/pwa/privateCaches.ts) share and that the unit tests check. Nothing here touches a cache.
//
// Three kinds of cache:
//   - the precache (Workbox): the app shell, the MapLibre worker, the glyphs and the sprites. The same for every
//     member, replaced with each release, never cleared on sign-out;
//   - the data cache: the map data (GET /api/osfm/places?…, /api/osfm/signatures?…) and the city index of the tiles
//     (/tiles/, /tiles/<city>.json), network first, at most dataCacheMaxEntries answers (one per area and filters);
//   - one tile cache per city: the basemap tiles, cache first, at most tileCacheMaxEntries each.
// The data and tile caches are private: their names start with privateCachePrefix, and they are deleted when the
// member signs out, when the session turns out to have ended, and before another member uses the device.

/** The prefix of every cache that holds what only a signed-in member may see. */
export const privateCachePrefix = 'osfm-private-'

/** The version of the private caches' layout; a service worker of another version deletes them on activation. */
const privateCacheVersion = 'v1'

/** The cache of the map data and the city index. */
export const dataCacheName = `${privateCachePrefix}data-${privateCacheVersion}`

const tileCachePrefix = `${privateCachePrefix}tiles-${privateCacheVersion}-`

/** The cache of one city's tiles. */
export function tileCacheName(city: string): string {
  return tileCachePrefix + city
}

/** Whether a cache holds private data (and so is deleted on sign-out). */
export function isPrivateCacheName(name: string): boolean {
  return name.startsWith(privateCachePrefix)
}

/** Whether a private cache belongs to an older (or newer) layout than this one: deleted when a version activates. */
export function isStalePrivateCacheName(name: string): boolean {
  return isPrivateCacheName(name) && name !== dataCacheName && !name.startsWith(tileCachePrefix)
}

/**
 * How long the network may take for map data before the cached answer is used. The network's answer, when it comes,
 * still updates the cache.
 */
export const networkTimeoutMillis = 3000

/**
 * The most tiles kept per city. A whole city extract is about 400 to 1600 tiles of 10 to 30 kB (z10–z15,
 * docs/architecture.md §3.6), so this keeps the parts of a city a member looks at, at most about 25 MB per city.
 */
export const tileCacheMaxEntries = 1000

/** When a tile cache is over its limit, the oldest tiles are deleted until this many are left. */
export const tileCacheTrimTo = 900

/**
 * The most answers kept in the data cache. Each area of the map and each combination of filters is an answer of its
 * own (up to a few hundred kB for a whole city of 1000 places).
 */
export const dataCacheMaxEntries = 60

/** When the data cache is over its limit, the oldest answers are deleted until this many are left. */
export const dataCacheTrimTo = 40

/**
 * A cached tile older than this (by its Date header) is fetched again. Tiles change only when an operator extracts a
 * city again; a sign-out empties the tile caches anyway.
 */
export const tileMaxAgeMillis = 30 * 24 * 60 * 60 * 1000

const cityPattern = /^[a-z0-9-]{1,64}$/
const tilePath = /^\/tiles\/([a-z0-9-]{1,64})\/\d{1,2}\/\d{1,7}\/\d{1,7}\.mvt$/
const tileJSONPath = /^\/tiles\/[a-z0-9-]{1,64}\.json$/

/** The map data the service worker handles network first. Everything else of the API it leaves to the browser. */
const dataPaths = new Set(['/api/osfm/places', '/api/osfm/signatures', '/tiles/'])

/** How the service worker handles a request: network first (data), cache first (a tile), or not at all (null). */
export type CacheRoute = { kind: 'data' } | { kind: 'tile'; city: string } | null

/**
 * The route of a request. Only GET requests to the app's own origin are handled; every other request, and every
 * other path of the API (the place card, the profile, notices, exports, backups: all no-store), reaches the network
 * as if there were no service worker.
 */
export function routeOf(method: string, url: URL, origin: string): CacheRoute {
  if (method !== 'GET' || url.origin !== origin) {
    return null
  }
  if (dataPaths.has(url.pathname) || tileJSONPath.test(url.pathname)) {
    return { kind: 'data' }
  }
  const tile = tilePath.exec(url.pathname)
  const city = tile?.[1]
  if (city !== undefined && cityPattern.test(city) && url.search === '') {
    return { kind: 'tile', city }
  }
  return null
}

/** Whether a Cache-Control header value forbids storing the response. */
export function forbidsStoring(cacheControl: string | null): boolean {
  return (cacheControl ?? '')
    .split(',')
    .map((d) => d.trim().toLowerCase())
    .some((d) => d === 'no-store')
}

/** What the service worker looks at to decide whether a response may go into a cache. */
export interface StorableResponse {
  status: number
  type: string
  headers: { get(name: string): string | null }
}

/**
 * Whether a response may be cached: a complete, successful same-origin answer that the server does not mark no-store.
 * Errors, redirects, 304s and 401s never are.
 */
export function isStorable(response: StorableResponse): boolean {
  return (
    response.status === 200 &&
    (response.type === 'basic' || response.type === 'default') &&
    !forbidsStoring(response.headers.get('Cache-Control')) &&
    response.headers.get('Vary')?.trim() !== '*'
  )
}

/** Whether a cached tile, by the Date header the server sent with it, is still young enough to be used. */
export function isTileFresh(date: string | null, now: number): boolean {
  if (!date) {
    return false
  }
  const at = Date.parse(date)
  return Number.isFinite(at) && now - at < tileMaxAgeMillis && at - now < tileMaxAgeMillis
}

/**
 * The entries to delete from a cache with a limit: none while it holds at most max entries; otherwise the oldest (by date,
 * then by their order in the cache) until trimTo are left. Entries without a usable date count as the oldest.
 */
export function evictions<K>(entries: { key: K; date: string | null }[], max: number, trimTo: number): K[] {
  if (entries.length <= max) {
    return []
  }
  const dated = entries.map((e, i) => {
    const at = e.date ? Date.parse(e.date) : NaN
    return { key: e.key, at: Number.isFinite(at) ? at : -Infinity, i }
  })
  dated.sort((a, b) => a.at - b.at || a.i - b.i)
  return dated.slice(0, entries.length - Math.max(0, trimTo)).map((e) => e.key)
}

/** The message the page sends to the service worker to delete the private caches; it answers on the given port. */
export const clearPrivateMessage = 'OSFM_CLEAR_PRIVATE'
