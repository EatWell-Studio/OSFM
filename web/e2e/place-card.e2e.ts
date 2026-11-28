// The place card on the shared instance (T029): a shared link opened signed out leads to the card after sign-in, the
// card switches the map to the place's city, centres the place and shows it and its recommendations as plain text, a
// landmark place jumps to its card, a tap on a pin opens its card beside the map, which stays in place and keeps its
// filters, a link opened directly centres the place rather than the view the map last showed, and a place the member
// may not see is not found. Opening a card asks no other origin: the watch of support/browser.ts fails the test on
// any such request (the source and profile links below are never fetched). Made-up data only.

import { expect, type Page, test } from '@playwright/test'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { city, zh } from './support/env.ts'
import {
  apiAs,
  authenticatorOf,
  createPlace,
  inviteMember,
  newSharedPerson,
  sharedInstance,
  type TestMember,
} from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.place
const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// Points in the city, apart from where other specs put their places.
const at = (x: number, y: number) => ({ lon: minLon + (maxLon - minLon) * x, lat: minLat + (maxLat - minLat) * y })

const bridgeSpot = at(0.41, 0.62)
const mainSpot = at(0.43, 0.6)
const positionNote = 'Test second floor <b>not bold</b>\n**behind** the [market](https://example.org/x)'
const recommendationText = 'Test must try <script>alert(1)</script>\nsecond line'
const sourceURL = 'https://example.org/test-source/1'
const pen = `Test Pen ${Date.now().toString(36)}`

let alice: TestMember
let bob: TestMember
let mainPlace: string
let bridgePlace: string
let privatePlace: string
let guest: Person
let bobPhone: Person
const people: Person[] = []

test.beforeAll(async ({ browser }) => {
  alice = await inviteMember()
  bob = await inviteMember()
  await apiAs(alice).post('/me/profile', { externalLinks: [{ url: 'https://example.org/test-alice', label: 'Test' }] })
  await apiAs(bob).post('/me/profile', { pseudonym: pen })

  bridgePlace = (
    await createPlace(bob, {
      name: 'Test Card Bridge Cafe',
      ...bridgeSpot,
      category: 'cafe',
      recommendation: { attitude: 'good', text: 'Test bridge text' },
    })
  ).place
  mainPlace = (
    await createPlace(alice, {
      name: 'Test Card Noodle House',
      aliases: ['Test Card Alias'],
      ...mainSpot,
      precision: 'approx',
      positionNote,
      landmark: { type: 'place', place: bridgePlace },
      category: 'restaurant',
      cuisine: ['noodle'],
      recommendation: {
        attitude: 'must_try',
        text: recommendationText,
        dishes: ['Test Dish'],
        timeSlots: ['lunch'],
        sources: [{ url: sourceURL, kind: 'chat', excerpt: 'Test excerpt' }],
      },
    })
  ).place
  await apiAs(bob).post(`/places/${mainPlace}/recommendations`, {
    attitude: 'avoid',
    text: 'Test pen name text',
    displayAs: 'pseudonym',
  })
  privatePlace = (
    await createPlace(alice, {
      name: 'Test Card Private Place',
      ...at(0.45, 0.58),
      recommendation: { attitude: 'good', text: 'Test private text', visibility: 'private' },
    })
  ).place

  guest = await newSharedPerson(browser, 'place-card-alice')
  bobPhone = await newSharedPerson(browser, 'place-card-bob', bob)
  await exposeMap(guest)
  await exposeMap(bobPhone)
  people.push(guest, bobPhone)
})

// eslint-disable-next-line no-empty-pattern -- Playwright needs the fixtures argument destructured
test.afterEach(async ({}, testInfo) => {
  await attachScreenshots(people, testInfo)
  // Includes every request to another origin: the source and profile links are never fetched.
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

// eslint-disable-next-line no-empty-pattern -- as above
test.afterAll(async ({}, testInfo) => {
  await close(people, testInfo.project.outputDir)
})

function url(path: string): RegExp {
  const escaped = (sharedInstance().baseURL + path).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`^${escaped}$`)
}

function card(page: Page, name: string) {
  return page.getByRole('complementary', { name })
}

/** The map as the page exposes it to the tests (src/map/places/PlaceLayers.tsx), with what this spec uses of it. */
interface MapLike {
  loaded(): boolean
  isMoving(): boolean
  getLayer(id: string): unknown
  jumpTo(options: { center: [number, number]; zoom: number }): void
  getZoom(): number
  getCenter(): { lng: number; lat: number }
  project(lngLat: [number, number]): { x: number; y: number }
  queryRenderedFeatures(options: { layers: string[] }): { properties: Record<string, unknown> }[]
  once(event: string, listener: () => void): void
}
type HookedWindow = Window & { osfmTestHooks?: { map?: MapLike } }

/** Lets the app hand its map to the test (the hook is inert unless the page defines it before the app starts). */
async function exposeMap(person: Person) {
  await person.context.addInitScript(() => {
    ;(window as HookedWindow).osfmTestHooks = {}
  })
}

async function openMap(page: Page, path: string) {
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

/** The names of the places drawn as pins (T028's layer), this spec's only. */
function rendered(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const map = (window as HookedWindow).osfmTestHooks?.map
    if (!map?.getLayer('osfm-place-pins')) {
      return []
    }
    const names = map.queryRenderedFeatures({ layers: ['osfm-place-pins'] }).map((f) => String(f.properties.name))
    return [...new Set(names)].filter((n) => n.startsWith('Test Card '))
  })
}

/** The map's centre and zoom, once it has stopped moving. */
async function view(page: Page): Promise<{ lon: number; lat: number; zoom: number }> {
  await page.waitForFunction(() => (window as HookedWindow).osfmTestHooks?.map?.isMoving() === false)
  return page.evaluate(() => {
    const map = (window as HookedWindow).osfmTestHooks?.map
    if (!map) {
      throw new Error('no map')
    }
    const c = map.getCenter()
    return { lon: c.lng, lat: c.lat, zoom: map.getZoom() }
  })
}

/** How far, in degrees, the map's centre is from the point while the map rests (Infinity while it moves). */
function offCentre(page: Page, point: { lon: number; lat: number }): Promise<number> {
  return page.evaluate((p) => {
    const map = (window as HookedWindow).osfmTestHooks?.map
    if (!map || map.isMoving()) {
      return Infinity
    }
    const c = map.getCenter()
    return Math.hypot(c.lng - p.lon, c.lat - p.lat)
  }, point)
}

test('a link opened signed out leads to the card after sign-in, in the place city', async () => {
  const { page } = guest
  await page.goto(`/p/${mainPlace}`)
  await expect(page).toHaveURL(url(`/login?next=${encodeURIComponent(`/p/${mainPlace}`)}`))

  const form = zh.auth.login.password
  await page.getByLabel(form.loginName, { exact: true }).fill(alice.loginName)
  await page.getByLabel(form.password, { exact: true }).fill(alice.password)
  await page.getByRole('button', { name: form.next, exact: true }).click()
  await page.getByLabel(form.totp, { exact: true }).fill(await authenticatorOf(alice).code())
  await page.getByRole('button', { name: form.signIn, exact: true }).click()

  // Back at the card, and the map shows the place's city (A7; ?city= is set only when the map showed another one).
  await expect(page).toHaveURL(new RegExp(`/p/${mainPlace}(\\?city=${city.slug})?$`))
  const shown = card(page, 'Test Card Noodle House')
  await expect(shown.getByRole('heading', { level: 2 })).toHaveText('Test Card Noodle House')
  await expect(page.locator('canvas.maplibregl-canvas')).toBeVisible()
  await expect(page.getByRole('combobox', { name: zh.map.city })).toHaveValue(city.slug)
  // Centred on the place (the server keeps 5 decimals of the coordinates).
  await expect.poll(() => offCentre(page, mainSpot)).toBeLessThan(2e-5)
})

test('the card shows the place and its recommendations as plain text', async () => {
  const { page } = guest
  const shown = card(page, 'Test Card Noodle House')
  await expect(shown.getByText('Test Card Alias')).toBeVisible()
  await expect(shown.getByText(zh.common.category.restaurant, { exact: true })).toBeVisible()
  await expect(shown.getByText(zh.common.cuisine.noodle, { exact: true })).toBeVisible()
  await expect(shown.getByText(t.approximate, { exact: true })).toBeVisible()
  await expect(shown.getByLabel(t.counts)).toHaveText(
    `${zh.common.attitude.must_try} 1 · ${zh.common.attitude.avoid} 1`,
  )
  // Verbatim, with its line break; no markup became elements.
  expect(await shown.locator('dd.whitespace-pre-line').first().textContent()).toBe(positionNote)
  expect(await shown.getByText('Test must try').textContent()).toBe(recommendationText)
  expect(await shown.locator('b, script:not([src]), strong, em').count()).toBe(0)
  // No coordinates anywhere.
  const { lat, lon } = mainSpot
  await expect(shown).not.toContainText(lat.toFixed(4))
  await expect(shown).not.toContainText(lon.toFixed(4))

  // Newest first: Bob's pseudonym recommendation, without links (E3), then Alice's with hers behind a toggle.
  const recs = shown.locator('li[id^="rec-"]')
  await expect(recs).toHaveCount(2)
  await expect(recs.nth(0)).toContainText(pen)
  await expect(recs.nth(0).getByRole('button')).toHaveCount(0)
  await expect(recs.nth(0).getByRole('link')).toHaveCount(0)
  await expect(recs.nth(1)).toContainText(alice.displayName)
  await expect(recs.nth(1).getByRole('button', { expanded: false })).toBeVisible()

  const source = recs.nth(1).getByRole('link', { name: /example\.org\/test-source\/1/ })
  await expect(source).toHaveAttribute('href', sourceURL)
  await expect(source).toHaveAttribute('target', '_blank')
  await expect(source).toHaveAttribute('rel', 'noopener noreferrer')
  await expect(recs.nth(1).getByText('Test excerpt')).toBeVisible()
  await expect(recs.nth(1).getByText('Test Dish', { exact: true })).toBeVisible()
})

test('a landmark place jumps to its card', async () => {
  const { page } = guest
  await card(page, 'Test Card Noodle House').getByRole('link', { name: 'Test Card Bridge Cafe' }).click()
  await expect(page).toHaveURL(new RegExp(`/p/${bridgePlace}(\\?city=${city.slug})?$`))
  await expect(card(page, 'Test Card Bridge Cafe').getByRole('heading', { level: 2 })).toHaveText(
    'Test Card Bridge Cafe',
  )
  // The map moves on to the other place.
  await expect.poll(() => offCentre(page, bridgeSpot)).toBeLessThan(2e-5)
})

test('a tap on a pin opens its card beside the map, centred; closing goes back without a jump', async () => {
  const { page } = bobPhone
  // The main place has an avoid: the filter leaves only the bridge cafe here, as a pin.
  await openMap(page, `/?city=${city.slug}&noAvoid=1`)
  await jumpTo(page, bridgeSpot, 15.5)
  await expect.poll(() => rendered(page)).toContain('Test Card Bridge Cafe')
  const canvas = page.locator('canvas.maplibregl-canvas')
  await canvas.evaluate((el) => {
    el.dataset.e2eKept = 'yes'
  })

  // The pin stands on its point: tap its head, a little above.
  const point = await page.evaluate((at) => (window as HookedWindow).osfmTestHooks?.map?.project(at), [
    bridgeSpot.lon,
    bridgeSpot.lat,
  ] as [number, number])
  const box = await canvas.boundingBox()
  if (!point || !box) {
    throw new Error('no map on the page')
  }
  await page.mouse.click(box.x + point.x, box.y + point.y - 15)

  await expect(page).toHaveURL(new RegExp(`/p/${bridgePlace}\\?`))
  expect(Object.fromEntries(new URL(page.url()).searchParams)).toEqual({ city: city.slug, noAvoid: '1' })
  const shown = card(page, 'Test Card Bridge Cafe')
  await expect(shown.getByRole('heading', { level: 2 })).toBeFocused()
  // The same map, centred on the place, its layers still there; the card lies beside the map, not over it, so the
  // attribution stays visible (A1).
  await expect(canvas).toHaveAttribute('data-e2e-kept', 'yes')
  await expect.poll(() => offCentre(page, bridgeSpot)).toBeLessThan(2e-5)
  await expect.poll(() => rendered(page)).toContain('Test Card Bridge Cafe')
  await expect(page.locator('.maplibregl-ctrl-attrib')).toBeInViewport()
  const before = await view(page)

  await shown.getByRole('button', { name: t.close }).click()
  await expect(page).toHaveURL(url(`/?city=${city.slug}&noAvoid=1`))
  await expect(page.getByRole('complementary')).toHaveCount(0)
  await expect(canvas).toHaveAttribute('data-e2e-kept', 'yes')
  // No jump: the map grew back around the same centre and zoom.
  await page.waitForTimeout(300)
  const after = await view(page)
  expect(after.zoom).toBeCloseTo(before.zoom, 5)
  expect(Math.hypot(after.lon - before.lon, after.lat - before.lat)).toBeLessThan(2e-5)
  await expect.poll(() => rendered(page)).toContain('Test Card Bridge Cafe')
})

test('a place link opened directly centres on the place, not on the view the map last showed', async () => {
  const { page } = bobPhone
  // The map remembers this view for the tab (T028); the link must win over it (A7).
  await jumpTo(page, at(0.85, 0.15), 13)
  await page.goto(`/p/${mainPlace}`)
  await expect(card(page, 'Test Card Noodle House').getByRole('heading', { level: 2 })).toBeVisible()
  await page.waitForFunction(() => (window as HookedWindow).osfmTestHooks?.map?.loaded() === true)
  await expect.poll(() => offCentre(page, mainSpot)).toBeLessThan(2e-5)
})

test('a place the member may not see cannot be found', async () => {
  const { page } = bobPhone
  await page.goto(`/p/${privatePlace}`)
  await expect(page.getByRole('heading', { level: 2, name: t.notFound })).toBeVisible()
  await expect(page.getByText('Test Card Private Place')).toHaveCount(0)
})
