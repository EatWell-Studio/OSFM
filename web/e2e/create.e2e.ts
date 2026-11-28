// New places and recommendations on the shared instance (T030): the shortest path (the map's "+", the crosshair, a
// name, an attitude, one sentence, save) saves a place that then shows on the map and in the list; a name like that
// of a place nearby offers that place, which leads to adding a recommendation there instead; a place search result
// moves the crosshair, and dragging the map afterwards makes the point the crosshair's again. The shortest path's
// steps and time are recorded as a reference (attachment create-shortest-path.json). The browser asks no other origin
// and the content security policy is never violated (support/browser.ts). Made-up data only.

import { expect, type Page, test } from '@playwright/test'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { city, zh } from './support/env.ts'
import { foundPlace } from './support/geocoder.ts'
import { createPlace, inviteMember, newSharedPerson, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.create
const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// Points in the city, apart from where other specs put their places.
const at = (x: number, y: number) => ({ lon: minLon + (maxLon - minLon) * x, lat: minLat + (maxLat - minLat) * y })
const newSpot = at(0.7, 0.3)
const dumplingSpot = at(0.75, 0.35)
const suffix = Date.now().toString(36)
const newName = `Test Create Noodles ${suffix}`
const dumplingName = `Test Create Dumpling Bar ${suffix}`

let alice: TestMember
let bob: TestMember
let dumplingPlace: string
let phone: Person
const people: Person[] = []

/** The map as the page exposes it to the tests (PlaceLayers.tsx, PinStep.tsx), with what this spec uses of it. */
interface MapLike {
  loaded(): boolean
  isMoving(): boolean
  getLayer(id: string): unknown
  jumpTo(options: { center: [number, number]; zoom: number }): void
  getCenter(): { lng: number; lat: number }
  queryRenderedFeatures(options: { layers: string[] }): { properties: Record<string, unknown> }[]
  once(event: string, listener: () => void): void
}
type HookedWindow = Window & { osfmTestHooks?: { map?: MapLike } }

test.beforeAll(async ({ browser }) => {
  alice = await inviteMember()
  bob = await inviteMember()
  dumplingPlace = (
    await createPlace(bob, {
      name: dumplingName,
      ...dumplingSpot,
      category: 'restaurant',
      recommendation: { attitude: 'good', text: 'Test dumplings' },
    })
  ).place
  phone = await newSharedPerson(browser, 'create-alice', alice)
  await phone.context.addInitScript(() => {
    ;(window as HookedWindow).osfmTestHooks = {}
  })
  people.push(phone)
})

// eslint-disable-next-line no-empty-pattern -- Playwright needs the fixtures argument destructured
test.afterEach(async ({}, testInfo) => {
  await attachScreenshots(people, testInfo)
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

// eslint-disable-next-line no-empty-pattern -- as above
test.afterAll(async ({}, testInfo) => {
  await close(people, testInfo.project.outputDir)
})

/** Forgets the map the page exposed, before the page shows another one. */
async function forgetMap(page: Page) {
  await page.evaluate(() => {
    const hooks = (window as HookedWindow).osfmTestHooks
    if (hooks) {
      hooks.map = undefined
    }
  })
}

/** Waits until the page shows a map that has loaded. */
async function mapLoaded(page: Page) {
  await expect(page.locator('canvas.maplibregl-canvas')).toBeVisible()
  await page.waitForFunction(() => (window as HookedWindow).osfmTestHooks?.map?.loaded() === true)
}

async function openMap(page: Page) {
  await page.goto(`/?city=${city.slug}`)
  await mapLoaded(page)
}

/** Moves the map without animation and waits until it has drawn everything (the map remembers the view, T028). */
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

/** The names of the places drawn as pins (T028's layer), this spec's only. */
function pins(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const map = (window as HookedWindow).osfmTestHooks?.map
    if (!map?.loaded() || !map.getLayer('osfm-place-pins')) {
      return []
    }
    const names = map.queryRenderedFeatures({ layers: ['osfm-place-pins'] }).map((f) => String(f.properties.name))
    return [...new Set(names)].filter((n) => n.startsWith('Test Create '))
  })
}

/** From the map to the second step: "+", then "here" with the crosshair where the map was. */
async function newPlaceHere(page: Page) {
  await forgetMap(page)
  await page.getByRole('link', { name: t.newPlace.button }).click()
  await expect(page).toHaveURL(/\/new$/)
  await mapLoaded(page)
  await page.getByRole('button', { name: t.pin.next }).click()
  await expect(page.getByRole('heading', { level: 1, name: t.newPlace.title })).toBeVisible()
}

function attitude(page: Page, label: string) {
  return page.getByRole('group', { name: t.recommendation.attitude }).getByText(label, { exact: true })
}

// eslint-disable-next-line no-empty-pattern -- as above
test('the shortest path saves a place, which shows on the map and in the list', async ({}, testInfo) => {
  const { page } = phone
  await openMap(page)
  await jumpTo(page, newSpot, 16)

  // Each step is one tap, or one tap and typing; the time runs from "+" to the new place's card.
  const steps: string[] = []
  const started = Date.now()
  await forgetMap(page)
  await page.getByRole('link', { name: t.newPlace.button }).click()
  steps.push('tap +')
  await mapLoaded(page)
  // The crosshair starts where the map was.
  await expect.poll(() => offCentre(page, newSpot)).toBeLessThan(1e-6)
  await page.getByRole('button', { name: t.pin.next }).click()
  steps.push('tap "here"')
  await page.getByLabel(t.place.name, { exact: true }).fill(newName)
  steps.push('type the name')
  await attitude(page, zh.common.attitude.must_try).click()
  steps.push('tap an attitude')
  await page.getByLabel(t.recommendation.text, { exact: true }).fill('Test hand-pulled noodles, one sentence')
  steps.push('type one sentence')
  await forgetMap(page)
  await page.getByRole('button', { name: t.newPlace.save, exact: true }).click()
  steps.push('tap save')
  const card = page.getByRole('complementary', { name: newName })
  await expect(card.getByRole('heading', { level: 2 })).toHaveText(newName)
  const millis = Date.now() - started
  const measurement = { steps: steps.length, stepList: steps, millis }
  console.log(`create: shortest path ${JSON.stringify(measurement)}`)
  await testInfo.attach('create-shortest-path.json', {
    body: JSON.stringify(measurement, null, 2),
    contentType: 'application/json',
  })

  await expect(card).toContainText(zh.common.attitude.must_try)
  await expect(card).toContainText('Test hand-pulled noodles, one sentence')
  await expect(card).toContainText(alice.displayName)
  // Not "add my recommendation" again: the member has one here.
  await expect(card.getByRole('link', { name: t.add.button })).toHaveCount(0)
  // On the map, as a pin of its own, centred.
  await mapLoaded(page)
  await expect.poll(() => offCentre(page, newSpot), { timeout: 10_000 }).toBeLessThan(2e-5)
  await expect.poll(() => pins(page), { timeout: 10_000 }).toContain(newName)
  // In the list.
  await page.getByRole('link', { name: zh.common.nav.list }).click()
  await expect(page.getByRole('list', { name: zh.map.list.places }).getByRole('link', { name: newName })).toBeVisible()
  // No coordinates anywhere on the way.
  expect(await page.locator('body').innerText()).not.toContain(newSpot.lat.toFixed(4))
})

test('a name like that of a place nearby offers it, and picking it adds a recommendation there', async () => {
  const { page } = phone
  await openMap(page)
  // About 10 m from the existing place.
  await jumpTo(page, { lon: dumplingSpot.lon + 0.0001, lat: dumplingSpot.lat }, 17)
  await newPlaceHere(page)
  await attitude(page, zh.common.attitude.ok).click()
  await page.getByLabel(t.place.name, { exact: true }).fill(dumplingName.toUpperCase())

  const hint = page.getByRole('region', { name: t.place.nearbyTitle })
  await expect(hint).toBeVisible()
  await expect(hint).toContainText(zh.common.category.restaurant)
  await expect(hint).toContainText(zh.map.list.metres.replace('{{value}}', '10'))
  await hint.getByRole('button', { name: new RegExp(dumplingName) }).click()

  await expect(page).toHaveURL(new RegExp(`/p/${dumplingPlace}/recommend$`))
  await expect(page.getByRole('heading', { level: 1, name: t.recommend.title })).toBeVisible()
  await expect(page.getByText(dumplingName, { exact: true })).toBeVisible()
  // The attitude chosen on the way came along.
  await expect(page.getByRole('radio', { name: zh.common.attitude.ok })).toBeChecked()
  await page.getByLabel(t.recommendation.text, { exact: true }).fill('Test dumplings, second opinion')
  await page.getByRole('button', { name: t.recommend.save, exact: true }).click()

  const card = page.getByRole('complementary', { name: dumplingName })
  await expect(card.getByText('Test dumplings, second opinion')).toBeVisible()
  await expect(card.getByLabel(zh.place.counts)).toHaveText(`${zh.common.attitude.good} 1 · ${zh.common.attitude.ok} 1`)

  // A second recommendation of the same place leads to editing the first (M2-Q1).
  await page.goto(`/p/${dumplingPlace}/recommend`)
  await expect(page.getByText(t.recommend.already)).toBeVisible()
  await expect(page.getByRole('link', { name: t.recommend.editMine })).toHaveAttribute(
    'href',
    new RegExp(`^/p/${dumplingPlace}/r/[0-9a-f-]{36}/edit$`),
  )
})

test('a search result moves the crosshair; dragging the map makes the point the crosshair again', async () => {
  const { page } = phone
  await openMap(page)
  await newPlaceHere(page)
  await forgetMap(page)
  await page.getByRole('button', { name: t.newPlace.adjust }).click()
  await mapLoaded(page)

  // A term of this spec's own: the instance caches answers, and search.e2e.ts counts what reaches the services.
  await page.getByRole('searchbox', { name: zh.search.label }).fill('Test Create Harbour')
  await page.getByRole('searchbox', { name: zh.search.label }).press('Enter')
  await page
    .getByRole('list', { name: zh.search.results })
    .getByRole('button', { name: new RegExp(foundPlace.name) })
    .click()
  await expect.poll(() => offCentre(page, foundPlace)).toBeLessThan(1e-6)
  await expect(page.getByText(t.pin.fromSearch)).toBeVisible()
  await page.getByRole('button', { name: t.pin.next }).click()
  await expect(page.getByText(t.newPlace.positionFromSearch)).toBeVisible()

  // Back to the crosshair, still on the search result; a drag by hand moves it off.
  await forgetMap(page)
  await page.getByRole('button', { name: t.newPlace.adjust }).click()
  await mapLoaded(page)
  await expect(page.getByText(t.pin.fromSearch)).toBeVisible()
  const box = await page.locator('canvas.maplibregl-canvas').boundingBox()
  if (!box) {
    throw new Error('no map on the page')
  }
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width / 2 + 60, box.y + box.height / 2 + 30, { steps: 8 })
  await page.mouse.up()
  await expect(page.getByText(t.pin.hint)).toBeVisible()
  await expect.poll(() => offCentre(page, foundPlace)).toBeGreaterThan(1e-5)
  await page.getByRole('button', { name: t.pin.next }).click()
  await expect(page.getByText(t.newPlace.positionSet)).toBeVisible()
})
