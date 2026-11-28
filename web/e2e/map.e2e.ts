// The places on the map, the filters and the list (T028), on the shared instance: places of the three precisions are
// drawn as a pin or as circles (checked with the features MapLibre rendered in each layer), the filters apply to the
// map and the list alike and stay when switching between them, and a tap opens the place's card with the filters kept.
// Then, on an instance of its own, the frame times of panning and zooming over 1000 places in headless Chrome (a
// reference value only; the devices are measured by hand). Made-up data only; nothing is asked of another origin.

import { mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import { type Browser, devices, expect, type Page, test, type TestInfo } from '@playwright/test'
import type { CreatePlaceRequest } from '../src/api/types.gen.ts'
import { attachScreenshots, close, newPerson, type Person, takeProblems } from './support/browser.ts'
import { city, zh } from './support/env.ts'
import { geocoderVariable } from './support/geocoder.ts'
import { startServer } from './support/server.ts'
import {
  ApiClient,
  createPlace,
  inviteMember,
  newSharedPerson,
  registerWithPassword,
  sessionCookie,
  setUpSharedInstance,
  type TestMember,
} from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.map
const common = zh.common

const layers = { pins: 'osfm-place-pins', circles: 'osfm-place-circles', clusters: 'osfm-place-clusters' }

const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// A corner of the city apart from where the other specs put their places; the three places 0.002° (about 220 m) apart.
const here = { lat: minLat + (maxLat - minLat) * 0.8, lon: minLon + (maxLon - minLon) * 0.25 }
const spots = {
  exact: { lat: here.lat, lon: here.lon },
  approx: { lat: here.lat, lon: here.lon + 0.002 },
  area: { lat: here.lat, lon: here.lon - 0.002 },
}
// Beyond the clustering (up to zoom 14), with all three in view.
const placeZoom = 15.5

let alice: TestMember
let bob: TestMember
let phone: Person
const people: Person[] = []
const uuids = { exact: '', approx: '', area: '' }
const names = { exact: 'Test Map Exact Noodles', approx: 'Test Map Approx Cafe', area: 'Test Map Area Stalls' }

/** The map as the page exposes it to the tests (src/map/places/PlaceLayers.tsx), with what the tests use of it. */
interface MapLike {
  loaded(): boolean
  isMoving(): boolean
  getLayer(id: string): unknown
  jumpTo(options: { center: [number, number]; zoom: number }): void
  easeTo(options: { center?: [number, number]; zoom?: number; duration: number }): void
  fitBounds(bounds: [number, number, number, number], options: { animate: boolean }): void
  getZoom(): number
  getCenter(): { lng: number; lat: number }
  project(lngLat: [number, number]): { x: number; y: number }
  queryRenderedFeatures(options: { layers: string[] }): { properties: Record<string, unknown> }[]
  getSource(id: string): { serialize(): { data: { features: unknown[] } } } | undefined
  once(event: string, listener: () => void): void
}
type HookedWindow = Window & { osfmTestHooks?: { map?: MapLike } }

/** Lets the app hand its map to the test (the hook is inert unless the page defines it before the app starts). */
async function exposeMap(person: Person) {
  await person.context.addInitScript(() => {
    ;(window as HookedWindow).osfmTestHooks = {}
  })
}

async function openMap(page: Page, path = '/') {
  await page.goto(path)
  await expect(page.locator('canvas.maplibregl-canvas')).toBeVisible()
  await page.waitForFunction(() => (window as HookedWindow).osfmTestHooks?.map?.loaded() === true)
}

/** Moves the map without animation and waits until it has drawn everything. */
async function jumpTo(page: Page, center: { lon: number; lat: number }, zoom: number) {
  await page.evaluate(
    ({ center, zoom }) =>
      new Promise<void>((done) => {
        const map = (window as HookedWindow).osfmTestHooks?.map
        if (!map) {
          throw new Error('no map')
        }
        map.once('idle', done)
        map.jumpTo({ center, zoom })
      }),
    { center: [center.lon, center.lat] as [number, number], zoom },
  )
}

/** The names of the places rendered in a layer, sorted; only this spec's places ("Test Map …"). */
function rendered(page: Page, layer: string): Promise<string[]> {
  return page.evaluate((layer) => {
    const map = (window as HookedWindow).osfmTestHooks?.map
    if (!map?.getLayer(layer)) {
      return []
    }
    // A feature near a tile's edge may be listed once per tile.
    const names = map.queryRenderedFeatures({ layers: [layer] }).map((f) => String(f.properties.name))
    return [...new Set(names)].filter((n) => n.startsWith('Test Map ')).sort()
  }, layer)
}

async function expectRendered(page: Page, expected: { pins: string[]; circles: string[] }) {
  await expect
    .poll(async () => ({ pins: await rendered(page, layers.pins), circles: await rendered(page, layers.circles) }))
    .toEqual({ pins: [...expected.pins].sort(), circles: [...expected.circles].sort() })
}

/** The filter button, whatever number of conditions it shows. */
function filterButton(page: Page) {
  return page.getByRole('button', { name: new RegExp(`^${t.filters.button}`) })
}

async function openFilters(page: Page) {
  await filterButton(page).click()
  return page.getByRole('dialog', { name: t.filters.title })
}

async function closeFilters(page: Page) {
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
}

/** The names in the list, in its order; only this spec's places. */
async function listed(page: Page): Promise<string[]> {
  const list = page.getByRole('list', { name: t.list.places })
  if ((await list.count()) === 0) {
    return []
  }
  const rows = await list.getByRole('link').allInnerTexts()
  return rows.map((r) => r.split('\n')[0] ?? '').filter((n) => n.startsWith('Test Map '))
}

function search(page: Page): URLSearchParams {
  return new URL(page.url()).searchParams
}

test.beforeAll(async ({ browser }) => {
  alice = await inviteMember()
  bob = await inviteMember()
  const made: [keyof typeof uuids, TestMember, CreatePlaceRequest][] = [
    [
      'exact',
      alice,
      {
        name: names.exact,
        ...spots.exact,
        category: 'restaurant',
        cuisine: ['noodle'],
        recommendation: { attitude: 'must_try', text: 'Test text, made up', timeSlots: ['lunch'] },
      },
    ],
    [
      'approx',
      alice,
      {
        name: names.approx,
        ...spots.approx,
        precision: 'approx',
        category: 'cafe',
        recommendation: { attitude: 'good', text: 'Test text, made up', timeSlots: ['breakfast'] },
      },
    ],
    [
      'area',
      bob,
      {
        name: names.area,
        ...spots.area,
        precision: 'area',
        category: 'street_vendor',
        recommendation: { attitude: 'avoid', text: 'Test text, made up', timeSlots: ['dinner'] },
      },
    ],
  ]
  for (const [key, member, place] of made) {
    uuids[key] = (await createPlace(member, place)).place
  }
  phone = await newSharedPerson(browser, 'map-member', alice)
  await exposeMap(phone)
  people.push(phone)
})

// eslint-disable-next-line no-empty-pattern -- Playwright needs the fixtures argument destructured
test.afterEach(async ({}, testInfo) => {
  await attachScreenshots(people, testInfo)
  // Includes every request to another origin.
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

// eslint-disable-next-line no-empty-pattern -- as above
test.afterAll(async ({}, testInfo) => {
  await close(people, testInfo.project.outputDir)
})

test('exact places are pins, approximate places and areas are circles; zoomed out they cluster', async () => {
  const { page } = phone
  const requests: string[] = []
  page.on('request', (r) => requests.push(r.url()))
  await openMap(page)

  await jumpTo(page, here, placeZoom)
  await expectRendered(page, { pins: [names.exact], circles: [names.approx, names.area] })
  // Each circle says what it is: the precision, and its size on the ground (radius at zoom 0, 300 m > 50 m).
  const circles = await page.evaluate((layer) => {
    const map = (window as HookedWindow).osfmTestHooks?.map
    return (map?.queryRenderedFeatures({ layers: [layer] }) ?? [])
      .filter((f) => String(f.properties.name).startsWith('Test Map '))
      .map((f) => ({ precision: String(f.properties.precision), r0: Number(f.properties.r0) }))
      .filter((c, i, all) => all.findIndex((o) => o.precision === c.precision) === i)
      .sort((a, b) => a.r0 - b.r0)
  }, layers.circles)
  expect(circles.map((c) => c.precision)).toEqual(['approx', 'area'])
  expect((circles[1]?.r0 ?? 0) / (circles[0]?.r0 ?? 1)).toBeCloseTo(6, 1)

  // At city level, the three are one cluster.
  await jumpTo(page, here, 12)
  await expect.poll(() => rendered(page, layers.pins)).toEqual([])
  const clustered = await page.evaluate((layer) => {
    const map = (window as HookedWindow).osfmTestHooks?.map
    return (map?.queryRenderedFeatures({ layers: [layer] }) ?? []).map((f) => Number(f.properties.point_count))
  }, layers.clusters)
  expect(clustered.some((n) => n >= 3)).toBe(true)

  // The places came from the instance, for the city, with a box.
  const asked = requests.filter((u) => u.includes('/api/osfm/places?'))
  expect(asked.length).toBeGreaterThan(0)
  const params = new URL(asked[0] ?? '').searchParams
  expect(params.get('city')).toBe(city.slug)
  expect(params.get('bbox')?.split(',')).toHaveLength(4)
})

test('the filters apply to the map and the list, and stay when switching between them', async () => {
  const { page } = phone
  await openMap(page)
  await jumpTo(page, here, placeZoom)
  await expectRendered(page, { pins: [names.exact], circles: [names.approx, names.area] })

  // Lunch: only the noodles.
  let sheet = await openFilters(page)
  await sheet
    .getByRole('group', { name: t.filters.slots })
    .getByRole('button', { name: common.timeSlot.lunch, exact: true })
    .click()
  await closeFilters(page)
  expect(search(page).get('slots')).toBe('lunch')
  await expect(page.getByRole('button', { name: t.filters.buttonCount_one.replace('{{count}}', '1') })).toBeVisible()
  await expectRendered(page, { pins: [names.exact], circles: [] })

  // The list: the same view and filters.
  const nav = page.getByRole('navigation', { name: common.nav.label })
  await nav.getByRole('link', { name: common.nav.list, exact: true }).click()
  await expect(page).toHaveURL(/\/list\?/)
  expect(search(page).get('slots')).toBe('lunch')
  await expect.poll(() => listed(page)).toEqual([names.exact])
  await expect(page.getByText(t.list.fromMapCenter)).toBeVisible()

  // In the list: no lunch any more, but nobody says avoid. The nearest first: the noodles are at the map's center.
  sheet = await openFilters(page)
  await sheet
    .getByRole('group', { name: t.filters.slots })
    .getByRole('button', { name: common.timeSlot.lunch, exact: true })
    .click()
  await sheet.getByRole('checkbox', { name: t.filters.noAvoid }).click()
  await closeFilters(page)
  await expect.poll(() => listed(page)).toEqual([names.exact, names.approx])
  const first = page.getByRole('list', { name: t.list.places }).getByRole('link').first()
  await expect(first).toContainText(common.attitude.must_try + ' 1')
  await expect(first).toContainText(common.category.restaurant)
  await expect(first).toContainText(t.list.metres.replace('{{value}}', '0'))

  // Back on the map, where it was left, with the list's filters.
  await nav.getByRole('link', { name: common.nav.map, exact: true }).click()
  await expect(page).toHaveURL(/\/\?noAvoid=1$/)
  await page.waitForFunction(() => (window as HookedWindow).osfmTestHooks?.map?.loaded() === true)
  await expectRendered(page, { pins: [names.exact], circles: [names.approx] })

  // By who recommended: Bob's area only (once the other filters are cleared).
  sheet = await openFilters(page)
  await sheet.getByRole('button', { name: t.filters.clear }).click()
  await sheet.getByRole('group', { name: t.filters.by }).getByRole('button', { name: bob.displayName }).click()
  await closeFilters(page)
  expect(search(page).getAll('by')).toEqual([bob.displayName])
  await expectRendered(page, { pins: [], circles: [names.area] })

  // Both conditions: nothing, and the map says so.
  sheet = await openFilters(page)
  await sheet.getByRole('checkbox', { name: t.filters.noAvoid }).click()
  await closeFilters(page)
  await expect(page.getByText(t.places.noMatch)).toBeVisible()
  await expectRendered(page, { pins: [], circles: [] })
})

test('a tap on a place opens its card, with the filters kept', async () => {
  const { page } = phone
  await openMap(page, '/?mustTry=1')
  await jumpTo(page, here, placeZoom)
  await expectRendered(page, { pins: [names.exact], circles: [] })

  // The pin stands on its point: tap its head, a little above.
  const point = await page.evaluate((at) => (window as HookedWindow).osfmTestHooks?.map?.project(at), [
    spots.exact.lon,
    spots.exact.lat,
  ] as [number, number])
  const canvas = await page.locator('canvas.maplibregl-canvas').boundingBox()
  if (!point || !canvas) {
    throw new Error('no map on the page')
  }
  await page.mouse.click(canvas.x + point.x, canvas.y + point.y - 15)
  await expect(page).toHaveURL(new RegExp(`/p/${uuids.exact}\\?mustTry=1$`))

  // From the list too.
  await page.goto('/list?noAvoid=1')
  await page
    .getByRole('list', { name: t.list.places })
    .getByRole('link', { name: new RegExp(names.approx) })
    .click()
  await expect(page).toHaveURL(new RegExp(`/p/${uuids.approx}\\?noAvoid=1$`))
})

// ---- 1000 places: frame times (reference value) ----

/** A free TCP port on 127.0.0.1. */
function freePort(): Promise<string> {
  return new Promise((done, fail) => {
    const server = createServer()
    server.on('error', fail)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      server.close(() => {
        if (address && typeof address === 'object') {
          done(String(address.port))
        } else {
          fail(new Error('no port'))
        }
      })
    })
  })
}

/** A small deterministic random generator (the same places every run). */
function random(seed: number): () => number {
  let state = seed
  return () => {
    state = (state * 1_103_515_245 + 12_345) % 2_147_483_648
    return state / 2_147_483_648
  }
}

const perfPlaces = 1000
const perfMembers = 20

/**
 * An instance of its own for the measurement, set up as the shared one is, with 1000 made-up places spread over the
 * city by 20 members (each within the per-member write budget). Its own, so that the other specs' maps stay small.
 */
async function startPerfInstance(testInfo: TestInfo) {
  const port = await freePort()
  const dir = mkdtempSync(join(tmpdir(), 'osfm-e2e-perf-'))
  const bin = resolve(
    process.env.OSFM_E2E_BINARY ?? join(import.meta.dirname, '..', 'node_modules', '.cache', 'osfm-e2e', 'osfm'),
  )
  const geocoder = process.env[geocoderVariable] ?? 'http://127.0.0.1:1'
  const server = await startServer(bin, port, join(dir, 'data'), testInfo.outputPath('osfm-server-perf.log'), {
    OSFM_GEOCODER_PHOTON_URL: `${geocoder}/photon/api`,
    OSFM_GEOCODER_NOMINATIM_URL: `${geocoder}/nominatim/search`,
  })
  const stop = async () => {
    await server.stop()
    rmSync(dir, { recursive: true, force: true })
  }
  try {
    const state = await setUpSharedInstance(port, join(dir, 'data'), server.setupCode)
    const maintainer = new ApiClient(port, { session: state.maintainer.session })
    const next = random(28)
    const pick = <T>(list: readonly T[]): T => list[Math.floor(next() * list.length)] as T
    const inset = 0.002
    let made = 0
    for (let m = 0; m < perfMembers; m++) {
      const invite = (await maintainer.post('/invites', { days: 1 })) as { code: string }
      const member = await registerWithPassword(
        new ApiClient(port),
        { start: '/register/password/start', confirm: '/register/password/confirm' },
        { invite: invite.code },
        {
          loginName: `test_perf_${String(m)}`,
          displayName: `Test Perf ${String(m)}`,
          password: 'test password, made up',
        },
      )
      const client = new ApiClient(port, { session: member.session })
      const count = perfPlaces / perfMembers
      const bodies: CreatePlaceRequest[] = []
      for (let i = 0; i < count; i++) {
        const r = next()
        bodies.push({
          name: `Test Perf Place ${String(made + i).padStart(4, '0')}`,
          lat: minLat + inset + next() * (maxLat - minLat - 2 * inset),
          lon: minLon + inset + next() * (maxLon - minLon - 2 * inset),
          precision: r < 0.7 ? 'exact' : r < 0.9 ? 'approx' : 'area',
          category: pick(['restaurant', 'fast_food', 'cafe', 'bakery', 'street_vendor', 'bar'] as const),
          recommendation: {
            attitude: pick(['must_try', 'good', 'ok', 'avoid'] as const),
            text: 'Test text, made up',
            timeSlots: [pick(['breakfast', 'lunch', 'afternoon', 'dinner', 'late_night'] as const)],
          },
        })
      }
      made += count
      for (let i = 0; i < bodies.length; i += 10) {
        await Promise.all(bodies.slice(i, i + 10).map((b) => client.post('/places', b)))
      }
    }
    return { baseURL: state.baseURL, session: state.maintainer.session, stop }
  } catch (e) {
    await stop()
    throw e
  }
}

interface FrameStats {
  frames: number
  meanMs: number
  p50Ms: number
  p95Ms: number
  p99Ms: number
  maxMs: number
  /** The percentage of frames longer than 1/30 s. */
  over33MsPercent: number
}

function stats(deltas: number[]): FrameStats {
  const sorted = [...deltas].sort((a, b) => a - b)
  const at = (q: number) => sorted[Math.min(sorted.length - 1, Math.floor(q * sorted.length))] ?? 0
  const round = (n: number) => Math.round(n * 10) / 10
  return {
    frames: sorted.length,
    meanMs: round(sorted.reduce((s, d) => s + d, 0) / Math.max(1, sorted.length)),
    p50Ms: round(at(0.5)),
    p95Ms: round(at(0.95)),
    p99Ms: round(at(0.99)),
    maxMs: round(sorted.at(-1) ?? 0),
    over33MsPercent: round((sorted.filter((d) => d > 1000 / 30).length / Math.max(1, sorted.length)) * 100),
  }
}

/** Records the time between animation frames while action runs. */
async function frameTimes(page: Page, action: () => Promise<void>): Promise<number[]> {
  await page.evaluate(() => {
    const w = window as Window & { frameDeltas?: number[]; recording?: boolean }
    w.frameDeltas = []
    w.recording = true
    let last = performance.now()
    const tick = (now: number) => {
      w.frameDeltas?.push(now - last)
      last = now
      if (w.recording) {
        requestAnimationFrame(tick)
      }
    }
    requestAnimationFrame((now) => {
      last = now
      requestAnimationFrame(tick)
    })
  })
  await action()
  return page.evaluate(() => {
    const w = window as Window & { frameDeltas?: number[]; recording?: boolean }
    w.recording = false
    return w.frameDeltas ?? []
  })
}

async function ease(page: Page, options: { center?: [number, number]; zoom?: number; duration: number }) {
  await page.evaluate(
    (options) =>
      new Promise<void>((done) => {
        const map = (window as HookedWindow).osfmTestHooks?.map
        if (!map) {
          throw new Error('no map')
        }
        map.once('moveend', done)
        map.easeTo(options)
      }),
    options,
  )
}

/** Drags the map with the mouse from the center by dx, dy CSS pixels, in small steps as a finger would. */
async function drag(page: Page, dx: number, dy: number) {
  const box = await page.locator('canvas.maplibregl-canvas').boundingBox()
  if (!box) {
    throw new Error('no map on the page')
  }
  const x = box.x + box.width / 2
  const y = box.y + box.height / 2
  await page.mouse.move(x, y)
  await page.mouse.down()
  await page.mouse.move(x + dx, y + dy, { steps: 30 })
  await page.mouse.up()
  await page.waitForTimeout(400)
}

async function measure(browser: Browser, testInfo: TestInfo) {
  const perf = await startPerfInstance(testInfo)
  const person = await newPerson(browser, 'map-perf', { ...devices['Pixel 7'], baseURL: perf.baseURL })
  try {
    await exposeMap(person)
    await person.context.addCookies([
      { name: sessionCookie, value: perf.session, url: perf.baseURL, httpOnly: true, sameSite: 'Lax' },
    ])
    const { page } = person
    const placesResponse = page.waitForResponse((r) => r.url().includes('/api/osfm/places?') && r.ok())
    await openMap(page)
    // The city is wider than the phone's screen is tall, so the map starts on part of it (its bounds limit zooming out),
    // and asks for the area around the view: most of the city.
    const loaded = ((await (await placesResponse).json()) as unknown[]).length
    expect(loaded).toBeGreaterThan(perfPlaces / 2)
    await expect
      .poll(
        () =>
          page.evaluate((source) => {
            const map = (window as HookedWindow).osfmTestHooks?.map
            return map?.getSource(source)?.serialize().data.features.length ?? 0
          }, 'osfm-places'),
        { timeout: 30_000 },
      )
      .toBe(loaded)
    await page.evaluate(
      () =>
        new Promise<void>((done) => {
          const map = (window as HookedWindow).osfmTestHooks?.map
          if (!map || (map.loaded() && !map.isMoving())) {
            done()
            return
          }
          map.once('idle', done)
        }),
    )

    const center = await page.evaluate(() => {
      const c = (window as HookedWindow).osfmTestHooks?.map?.getCenter()
      return [c?.lng ?? 0, c?.lat ?? 0] as [number, number]
    })
    const startZoom = await page.evaluate(() => (window as HookedWindow).osfmTestHooks?.map?.getZoom() ?? 0)

    /** The same moves every time, from the same view: frame statistics per phase. */
    const sequence = async () => {
      await ease(page, { center, zoom: startZoom, duration: 0 })
      const phases: Record<string, FrameStats> = {}
      const all: number[] = []
      const run = async (name: string, action: () => Promise<void>) => {
        const deltas = await frameTimes(page, action)
        phases[name] = stats(deltas)
        all.push(...deltas)
      }
      await run('idle, 1 s', () => page.waitForTimeout(1000))
      await run('pan, city level (clusters)', async () => {
        for (const [dx, dy] of [
          [120, 0],
          [-240, 80],
          [120, -160],
          [0, 80],
        ] as const) {
          await drag(page, dx, dy)
        }
      })
      await run('zoom to street level and back (animated, 2 × 1.5 s)', async () => {
        await ease(page, { center, zoom: 15.5, duration: 1500 })
        await ease(page, { center, zoom: startZoom, duration: 1500 })
      })
      await ease(page, { center, zoom: 16.5, duration: 0 })
      await run('pan, street level (pins, circles and names)', async () => {
        for (const [dx, dy] of [
          [150, 0],
          [-300, 100],
          [150, -200],
          [0, 100],
        ] as const) {
          await drag(page, dx, dy)
        }
      })
      return { phases, overall: stats(all) }
    }

    const withPlaces = await sequence()
    // The same without any place (a filter that matches none), for comparison: what the basemap alone costs here.
    await openMap(page, `/?by=${encodeURIComponent('Test Nobody')}`)
    await expect
      .poll(() =>
        page.evaluate(
          (source) =>
            (window as HookedWindow).osfmTestHooks?.map?.getSource(source)?.serialize().data.features.length ?? -1,
          'osfm-places',
        ),
      )
      .toBe(0)
    const basemapOnly = await sequence()
    const result = { places: perfPlaces, loaded, browser: browser.version(), withPlaces, basemapOnly }
    console.log(`map frame times with ${String(perfPlaces)} places:\n${JSON.stringify(result, null, 2)}`)
    writeFileSync(testInfo.outputPath('map-frame-times.json'), JSON.stringify(result, null, 2))
    await testInfo.attach('map-frame-times.json', {
      body: JSON.stringify(result, null, 2),
      contentType: 'application/json',
    })
    expect(takeProblems([person]), 'CSP violations, requests to other origins or page errors').toEqual([])
    expect(result.withPlaces.overall.frames).toBeGreaterThan(30)
  } finally {
    await close([person], testInfo.project.outputDir)
    await perf.stop()
  }
}

test('1000 places: frame times while panning and zooming (reference values)', async ({ browser }, testInfo) => {
  test.setTimeout(300_000)
  await measure(browser, testInfo)
})
