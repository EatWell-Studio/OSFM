/// <reference lib="webworker" />
// The service worker (docs/architecture.md §4.5), built by vite-plugin-pwa (injectManifest) into /sw.js. The rules of
// what is cached where are in rules.ts; this file applies them.
//
//   - Precache (Workbox): the app shell, the MapLibre worker, the glyphs and the sprites, cache first, replaced with
//     each release. Navigations inside the app are answered with the precached index.html.
//   - Map data and the city index: network first. A cached answer is revalidated with its ETag (If-None-Match; a 304
//     keeps the cached copy); when the network has not answered within networkTimeoutMillis, or fails, the cached
//     copy is used and the network's later answer still updates the cache. An HTTP error (a 401 above all) is never
//     replaced by a cached copy. At most dataCacheMaxEntries answers are kept.
//   - Tiles: cache first, one cache per city with at most tileCacheMaxEntries tiles.
//   - Nothing else: the rest of the API (all no-store) and every other request go to the network untouched.
//
// The service worker's own requests bypass the browser's HTTP cache (cache: 'no-store'), so that the only copies of
// private responses are in the private caches, which the page deletes on sign-out (src/pwa/privateCaches.ts).

import { cleanupOutdatedCaches, createHandlerBoundToURL, precacheAndRoute } from 'workbox-precaching'
import { NavigationRoute, registerRoute } from 'workbox-routing'
import {
  clearPrivateMessage,
  dataCacheName,
  isPrivateCacheName,
  isStalePrivateCacheName,
  isStorable,
  isTileFresh,
  networkTimeoutMillis,
  routeOf,
  tileCacheMaxEntries,
  tileCacheName,
  tileCacheTrimTo,
  dataCacheMaxEntries,
  dataCacheTrimTo,
  evictions,
} from './rules'

declare const self: ServiceWorkerGlobalScope & { __WB_MANIFEST: (string | { url: string; revision: string | null })[] }

/**
 * Bumped whenever the private caches are deleted: a response that was on its way while they were deleted is not put
 * into a cache afterwards (it belongs to the session that just ended).
 */
let generation = 0

precacheAndRoute(self.__WB_MANIFEST)
cleanupOutdatedCaches()

// Paths of the app are answered with the app's page; the server's own paths never are.
registerRoute(
  new NavigationRoute(createHandlerBoundToURL('index.html'), {
    denylist: [/^\/api(\/|$)/, /^\/tiles(\/|$)/, /^\/_(\/|$)/, /^\/\.well-known(\/|$)/],
  }),
)

/** The request, sent past the HTTP cache, with headers added. */
function bypassingHTTPCache(request: Request, headers?: Record<string, string>): Request {
  const h = new Headers(request.headers)
  for (const [name, value] of Object.entries(headers ?? {})) {
    h.set(name, value)
  }
  return new Request(request, { headers: h, cache: 'no-store' })
}

function putLater(event: ExtendableEvent, cacheName: string, request: Request, response: Response, started: number) {
  if (!isStorable(response) || started !== generation) {
    return
  }
  const copy = response.clone()
  event.waitUntil(
    caches
      .open(cacheName)
      .then(async (cache) => {
        if (started === generation) {
          await cache.put(request, copy)
        }
      })
      .catch(() => undefined),
  )
}

const timeout = (ms: number) =>
  new Promise<'timeout'>((done) => {
    setTimeout(() => {
      done('timeout')
    }, ms)
  })

async function networkFirst(event: FetchEvent): Promise<Response> {
  const request = event.request
  const started = generation
  const cache = await caches.open(dataCacheName)
  const cached = await cache.match(request, { ignoreVary: true })
  const etag = cached?.headers.get('ETag')
  const network = fetch(bypassingHTTPCache(request, etag ? { 'If-None-Match': etag } : undefined)).then((response) => {
    if (response.status === 304 && cached) {
      return cached.clone()
    }
    if (isStorable(response)) {
      putLater(event, dataCacheName, request, response, started)
      scheduleTrim(event, dataCacheName, dataCacheMaxEntries, dataCacheTrimTo)
    }
    return response
  })
  // The network's answer is awaited even after the cached copy was used, so that it can update the cache.
  event.waitUntil(
    network.then(
      () => undefined,
      () => undefined,
    ),
  )
  if (!cached) {
    return network
  }
  try {
    const first = await Promise.race([network, timeout(networkTimeoutMillis)])
    return first === 'timeout' ? cached.clone() : first
  } catch {
    // Offline, or the connection failed.
    return cached.clone()
  }
}

/** Per cache, the trim that is due (at most one at a time). */
const trims = new Map<string, Promise<void>>()

/** Deletes the oldest entries (by their Date header) of a cache over its limit. */
async function trim(cacheName: string, max: number, trimTo: number) {
  const cache = await caches.open(cacheName)
  const keys = await cache.keys()
  if (keys.length <= max) {
    return
  }
  const entries = await Promise.all(
    keys.map(async (key) => ({ key, date: (await cache.match(key))?.headers.get('Date') ?? null })),
  )
  for (const key of evictions(entries, max, trimTo)) {
    await cache.delete(key)
  }
}

/** Trims the cache a second after a write; the writes meanwhile (the other tiles of a view) share that trim. */
function scheduleTrim(event: ExtendableEvent, cacheName: string, max: number, trimTo: number) {
  if (trims.has(cacheName)) {
    return
  }
  const due = timeout(1000)
    .then(() => trim(cacheName, max, trimTo))
    .catch(() => undefined)
    .finally(() => trims.delete(cacheName))
  trims.set(cacheName, due)
  event.waitUntil(due)
}

async function cacheFirst(event: FetchEvent, city: string): Promise<Response> {
  const request = event.request
  const started = generation
  const cacheName = tileCacheName(city)
  const cache = await caches.open(cacheName)
  const cached = await cache.match(request, { ignoreVary: true })
  if (cached && isTileFresh(cached.headers.get('Date'), Date.now())) {
    return cached
  }
  try {
    const response = await fetch(bypassingHTTPCache(request))
    if (isStorable(response)) {
      putLater(event, cacheName, request, response, started)
      scheduleTrim(event, cacheName, tileCacheMaxEntries, tileCacheTrimTo)
    }
    return response
  } catch (e) {
    if (cached) {
      return cached
    }
    throw e
  }
}

self.addEventListener('fetch', (event) => {
  const route = routeOf(event.request.method, new URL(event.request.url), self.location.origin)
  if (route?.kind === 'data') {
    event.respondWith(networkFirst(event))
  } else if (route?.kind === 'tile') {
    event.respondWith(cacheFirst(event, route.city))
  }
})

async function deletePrivateCaches(stale: (name: string) => boolean) {
  const names = await caches.keys()
  await Promise.all(names.filter(stale).map((name) => caches.delete(name)))
}

self.addEventListener('message', (event) => {
  const type = (event.data as { type?: unknown } | null)?.type
  if (type === 'SKIP_WAITING') {
    // The page's "reload" on the new-version prompt (vite-plugin-pwa's updateServiceWorker).
    void self.skipWaiting()
  } else if (type === clearPrivateMessage) {
    generation++
    const port = event.ports[0]
    event.waitUntil(
      deletePrivateCaches(isPrivateCacheName).then(
        () => port?.postMessage({ ok: true }),
        () => port?.postMessage({ ok: false }),
      ),
    )
  }
})

self.addEventListener('activate', (event) => {
  // The first version takes over the open pages at once, so that a first visit is cached already; a later version
  // waits for the prompt (or for every page to close) and is then in control after the reload.
  event.waitUntil(Promise.all([self.clients.claim(), deletePrivateCaches(isStalePrivateCacheName)]))
})
