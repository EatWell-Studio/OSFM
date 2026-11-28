import { describe, expect, it } from 'vitest'
import {
  dataCacheName,
  forbidsStoring,
  isPrivateCacheName,
  isStalePrivateCacheName,
  isStorable,
  isTileFresh,
  routeOf,
  tileCacheName,
  evictions,
  tileMaxAgeMillis,
} from './rules'

const origin = 'https://food.example'
const route = (path: string, method = 'GET', from = origin) => routeOf(method, new URL(path, from), origin)

describe('routeOf', () => {
  it('handles the map data and the city index network first', () => {
    expect(route('/api/osfm/places?city=example-bay&bbox=1,2,3,4&slots=lunch')).toEqual({ kind: 'data' })
    expect(route('/api/osfm/places?city=example-bay')).toEqual({ kind: 'data' })
    expect(route('/api/osfm/signatures?city=example-bay')).toEqual({ kind: 'data' })
    expect(route('/tiles/')).toEqual({ kind: 'data' })
    expect(route('/tiles/example-bay.json')).toEqual({ kind: 'data' })
  })

  it('handles tiles cache first, by city', () => {
    expect(route('/tiles/example-bay/12/2149/2031.mvt')).toEqual({ kind: 'tile', city: 'example-bay' })
    expect(route('/tiles/x1/0/0/0.mvt')).toEqual({ kind: 'tile', city: 'x1' })
  })

  it('leaves everything else to the network', () => {
    for (const path of [
      // no-store answers: the place card, the profile and session, notices, exports, backups, the instance settings
      '/api/osfm/places/0190f5a0-0000-7000-8000-000000000001',
      '/api/osfm/places/nearby?lat=1&lon=2&name=x',
      '/api/osfm/me',
      '/api/osfm/me/notices',
      '/api/osfm/me/export',
      '/api/osfm/admin/backup',
      '/api/osfm/instance',
      '/api/osfm/places/',
      // not tiles of a city
      '/tiles',
      '/tiles/Example/1/2/3.mvt',
      '/tiles/example-bay/1/2/3.png',
      '/tiles/example-bay/1/2/3.mvt?v=1',
      '/tiles/../api/1/2/3.mvt',
      // the app itself is the precache's
      '/',
      '/index.html',
      '/map/fonts/Noto%20Sans%20Regular/0-255.pbf',
    ]) {
      expect(route(path), path).toBeNull()
    }
  })

  it('handles only GET requests to the own origin', () => {
    expect(route('/api/osfm/places?city=x', 'POST')).toBeNull()
    expect(route('/tiles/x/1/2/3.mvt', 'HEAD')).toBeNull()
    expect(route('/tiles/x/1/2/3.mvt', 'GET', 'https://elsewhere.example')).toBeNull()
    expect(route('/api/osfm/places?city=x', 'GET', 'http://food.example')).toBeNull()
  })
})

describe('storing', () => {
  const response = (status: number, cacheControl: string | null, type = 'basic', vary: string | null = null) => ({
    status,
    type,
    headers: new Headers([
      ...(cacheControl === null ? [] : [['Cache-Control', cacheControl] as [string, string]]),
      ...(vary === null ? [] : [['Vary', vary] as [string, string]]),
    ]),
  })

  it('reads no-store out of Cache-Control', () => {
    expect(forbidsStoring('no-store')).toBe(true)
    expect(forbidsStoring('private, No-Store')).toBe(true)
    expect(forbidsStoring('private, no-cache')).toBe(false)
    expect(forbidsStoring('private, max-age=86400')).toBe(false)
    expect(forbidsStoring(null)).toBe(false)
  })

  it('stores complete successful answers the server allows to be stored', () => {
    expect(isStorable(response(200, 'private, no-cache', 'basic', 'Cookie'))).toBe(true)
    expect(isStorable(response(200, 'private, max-age=86400'))).toBe(true)
    expect(isStorable(response(200, null, 'default'))).toBe(true)
  })

  it('never stores no-store answers, errors, partial or opaque answers', () => {
    expect(isStorable(response(200, 'no-store'))).toBe(false)
    expect(isStorable(response(200, 'private, no-store'))).toBe(false)
    for (const status of [204, 206, 301, 304, 401, 403, 404, 500, 503]) {
      expect(isStorable(response(status, 'private, no-cache')), String(status)).toBe(false)
    }
    expect(isStorable(response(200, null, 'opaque'))).toBe(false)
    expect(isStorable(response(200, null, 'cors'))).toBe(false)
    expect(isStorable(response(200, null, 'basic', '*'))).toBe(false)
  })
})

describe('caches', () => {
  it('names the private caches with one prefix', () => {
    expect(isPrivateCacheName(dataCacheName)).toBe(true)
    expect(isPrivateCacheName(tileCacheName('example-bay'))).toBe(true)
    expect(isPrivateCacheName('workbox-precache-v2-https://food.example/')).toBe(false)
  })

  it('tells caches of another layout apart', () => {
    expect(isStalePrivateCacheName(dataCacheName)).toBe(false)
    expect(isStalePrivateCacheName(tileCacheName('example-bay'))).toBe(false)
    expect(isStalePrivateCacheName('osfm-private-data-v0')).toBe(true)
    expect(isStalePrivateCacheName('osfm-private-tiles-v0-example-bay')).toBe(true)
    expect(isStalePrivateCacheName('workbox-precache-v2-https://food.example/')).toBe(false)
  })
})

describe('tiles', () => {
  const now = Date.parse('2026-11-28T12:00:00Z')

  it('uses a cached tile while it is younger than the limit', () => {
    expect(isTileFresh(new Date(now - 1000).toUTCString(), now)).toBe(true)
    expect(isTileFresh(new Date(now - tileMaxAgeMillis + 60_000).toUTCString(), now)).toBe(true)
    expect(isTileFresh(new Date(now - tileMaxAgeMillis - 60_000).toUTCString(), now)).toBe(false)
    expect(isTileFresh(null, now)).toBe(false)
    expect(isTileFresh('not a date', now)).toBe(false)
  })

  it('evicts nothing within the limit', () => {
    const entries = [1, 2, 3].map((key) => ({ key, date: new Date(now - key * 1000).toUTCString() }))
    expect(evictions(entries, 3, 2)).toEqual([])
  })

  it('evicts the oldest down to the trim size, undated first', () => {
    const entries = [
      { key: 'b', date: new Date(now - 2000).toUTCString() },
      { key: 'new', date: new Date(now).toUTCString() },
      { key: 'undated', date: null },
      { key: 'a', date: new Date(now - 5000).toUTCString() },
      { key: 'c', date: new Date(now - 1000).toUTCString() },
    ]
    expect(evictions(entries, 4, 2)).toEqual(['undated', 'a', 'b'])
  })

  it('keeps the order in the cache for tiles of the same date', () => {
    const date = new Date(now).toUTCString()
    expect(
      evictions(
        ['x', 'y', 'z'].map((key) => ({ key, date })),
        2,
        1,
      ),
    ).toEqual(['x', 'y'])
  })
})
