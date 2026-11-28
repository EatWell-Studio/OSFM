// The place card on the shared instance (T029): a shared link opened signed out leads to the card after sign-in, the
// card switches the map to the place's city and shows the place and its recommendations as plain text, a landmark
// place jumps to its card, a card opens over the map that stays in place, and a place the member may not see is not
// found. Opening a card asks no other origin: the watch of support/browser.ts fails the test on any such request (the
// source and profile links below are never fetched). Made-up data only.

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
      ...at(0.41, 0.62),
      category: 'cafe',
      recommendation: { attitude: 'good', text: 'Test bridge text' },
    })
  ).place
  mainPlace = (
    await createPlace(alice, {
      name: 'Test Card Noodle House',
      aliases: ['Test Card Alias'],
      ...at(0.43, 0.6),
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

  // Back at the card, and the map switched to the place's city (A7).
  await expect(page).toHaveURL(url(`/p/${mainPlace}?city=${city.slug}`))
  const shown = card(page, 'Test Card Noodle House')
  await expect(shown.getByRole('heading', { level: 2 })).toHaveText('Test Card Noodle House')
  await expect(page.locator('canvas.maplibregl-canvas')).toBeVisible()
  await expect(page.getByRole('combobox', { name: zh.map.city })).toHaveValue(city.slug)
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
  const { lat, lon } = at(0.43, 0.6)
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
  await expect(page).toHaveURL(url(`/p/${bridgePlace}?city=${city.slug}`))
  await expect(card(page, 'Test Card Bridge Cafe').getByRole('heading', { level: 2 })).toHaveText(
    'Test Card Bridge Cafe',
  )
})

test('a card opens on the map, which stays in place, and closes back to it', async () => {
  const { page } = bobPhone
  await page.goto('/')
  const canvas = page.locator('canvas.maplibregl-canvas')
  await expect(canvas).toBeVisible()
  await canvas.evaluate((el) => {
    el.dataset.e2eKept = 'yes'
  })

  // Map features lead to placePath(uuid) with the query kept (T028); here the app navigates there the same way.
  await page.evaluate((path) => {
    window.history.pushState(null, '', path)
    window.dispatchEvent(new PopStateEvent('popstate'))
  }, `/p/${mainPlace}?city=${city.slug}`)
  const shown = card(page, 'Test Card Noodle House')
  await expect(shown.getByRole('heading', { level: 2 })).toBeFocused()
  await expect(canvas).toHaveAttribute('data-e2e-kept', 'yes')
  // The card lies beside the map, not over it: the attribution stays visible (A1).
  await expect(page.locator('.maplibregl-ctrl-attrib')).toBeInViewport()

  await shown.getByRole('button', { name: t.close }).click()
  await expect(page).toHaveURL(url(`/?city=${city.slug}`))
  await expect(page.getByRole('complementary')).toHaveCount(0)
  await expect(canvas).toHaveAttribute('data-e2e-kept', 'yes')
})

test('a place the member may not see cannot be found', async () => {
  const { page } = bobPhone
  await page.goto(`/p/${privatePlace}`)
  await expect(page.getByRole('heading', { level: 2, name: t.notFound })).toBeVisible()
  await expect(page.getByText('Test Card Private Place')).toHaveCount(0)
})
