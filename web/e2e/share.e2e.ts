// Navigation and sharing on the place card (T032): the card links the place to Amap, Apple Maps and Google Maps without
// the page requesting anything from them; "share with the group" hands a text that ends in the place's link (with ?r=
// for one recommendation) to the share sheet, or copies it where there is none; a shared link opened signed out leads
// to the card after sign-in, scrolled to the recommendation it names and highlighting it. The watch of
// support/browser.ts fails a test on any request to another origin. Made-up data only.

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

const t = zh.share
const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// A point in the city, apart from where other specs put their places; the made-up city lies outside mainland China,
// so every app gets these coordinates unchanged.
const spot = {
  lon: Number((minLon + (maxLon - minLon) * 0.67).toFixed(5)),
  lat: Number((minLat + (maxLat - minLat) * 0.27).toFixed(5)),
}
const placeName = `Test Share Noodle Bar ${Date.now().toString(36)}`

let alice: TestMember
let bob: TestMember
let carol: TestMember
let place: string
let bobsRecommendation: string
let alicePhone: Person
let carolLaptop: Person
let guest: Person
const people: Person[] = []

type SharingWindow = Window & { osfmShared?: ShareData[] }

test.beforeAll(async ({ browser }) => {
  alice = await inviteMember()
  bob = await inviteMember()
  carol = await inviteMember()
  place = (
    await createPlace(alice, {
      name: placeName,
      ...spot,
      positionNote: 'Test corner\nby the bridge',
      recommendation: { attitude: 'must_try', text: 'Test noodles, the best in town' },
    })
  ).place
  const added = (await apiAs(bob).post(`/places/${place}/recommendations`, {
    attitude: 'good',
    text: `Test ${'long text '.repeat(10)}tail`,
  })) as { recommendation: string }
  bobsRecommendation = added.recommendation

  // A phone with a share sheet that records what it is given.
  alicePhone = await newSharedPerson(browser, 'share-alice', alice)
  await alicePhone.context.addInitScript(() => {
    const w = window as SharingWindow
    w.osfmShared = []
    Object.defineProperty(Navigator.prototype, 'share', {
      configurable: true,
      value: (data: ShareData) => {
        w.osfmShared?.push(data)
        return Promise.resolve()
      },
    })
  })
  // A laptop without one: the text goes to the clipboard.
  carolLaptop = await newSharedPerson(browser, 'share-carol', carol, {
    viewport: { width: 1280, height: 800 },
    permissions: ['clipboard-read', 'clipboard-write'],
  })
  await carolLaptop.context.addInitScript(() => {
    Reflect.deleteProperty(Navigator.prototype, 'share')
  })
  guest = await newSharedPerson(browser, 'share-guest')
  people.push(alicePhone, carolLaptop, guest)
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

function card(page: Page) {
  return page.getByRole('complementary', { name: placeName })
}

function link(rec?: string): string {
  const base = `${sharedInstance().baseURL}/p/${place}`
  return rec ? `${base}?r=${rec}` : base
}

const navigationHosts = /(^|\.)(amap\.com|apple\.com|google\.[a-z.]+)$/

test('the card links the place to three navigation apps, and loading it asks none of them', async () => {
  const { page } = alicePhone
  const hosts: string[] = []
  page.on('request', (request) => {
    hosts.push(new URL(request.url()).hostname)
  })
  await page.goto(`/p/${place}`)
  const shown = card(page)
  await expect(shown.getByRole('heading', { level: 2 })).toHaveText(placeName)
  const apps = shown.getByRole('group', { name: t.navigate }).getByRole('link')
  await expect(apps).toHaveCount(3)

  const name = encodeURIComponent(placeName)
  const { lat, lon } = spot
  const expected = [
    `https://uri.amap.com/marker?position=${String(lon)},${String(lat)}&name=${name}&coordinate=wgs84&callnative=1`,
    `https://maps.apple.com/?ll=${String(lat)},${String(lon)}&q=${name}`,
    `https://www.google.com/maps/search/?api=1&query=${String(lat)}%2C${String(lon)}`,
  ]
  for (const [i, href] of expected.entries()) {
    const app = apps.nth(i)
    await expect(app).toHaveAttribute('href', href)
    await expect(app).toHaveAttribute('target', '_blank')
    await expect(app).toHaveAttribute('rel', 'noopener noreferrer')
  }
  await expect(apps.nth(0)).toContainText(t.app.amap)
  await expect(apps.nth(1)).toContainText(t.app.apple)
  await expect(apps.nth(2)).toContainText(t.app.google)

  // Wait for the map too, then check: nothing went to a navigation app (nor anywhere else, see afterEach).
  await expect(page.locator('canvas.maplibregl-canvas')).toBeVisible()
  await page.waitForLoadState('networkidle')
  expect(hosts.filter((h) => navigationHosts.test(h))).toEqual([])
  expect(hosts.length).toBeGreaterThan(0)
})

test('sharing the place hands its text and link to the share sheet', async () => {
  const { page } = alicePhone
  await card(page).getByRole('button', { name: t.sharePlace }).click()
  await expect.poll(() => page.evaluate(() => (window as SharingWindow).osfmShared?.length)).toBe(1)
  const [shared] = await page.evaluate(() => (window as SharingWindow).osfmShared ?? [])
  const lines = (shared?.text ?? '').split('\n')
  expect(lines).toEqual([
    [
      t.text.placeWithCity.replace('{{name}}', placeName).replace('{{city}}', city.name),
      'Test corner by the bridge',
      `${zh.common.attitude.must_try} 1`,
      `${zh.common.attitude.good} 1`,
    ].join(t.text.separator),
    link(),
  ])
  expect(shared?.url).toBeUndefined()
})

test('sharing one recommendation adds it, shortened, and links to it', async () => {
  const { page } = alicePhone
  const rec = page.locator(`li[id="rec-${bobsRecommendation}"]`)
  await rec.getByRole('button', { name: t.shareRecommendation.replace('{{name}}', bob.displayName) }).click()
  await expect.poll(() => page.evaluate(() => (window as SharingWindow).osfmShared?.length)).toBe(2)
  const shared = await page.evaluate(() => (window as SharingWindow).osfmShared?.[1])
  const lines = (shared?.text ?? '').split('\n')
  expect(lines).toHaveLength(3)
  const quoted = `Test ${'long text '.repeat(10)}tail`.slice(0, 60)
  expect(lines[1]).toBe(
    t.text.recommendation
      .replace('{{attitude}}', zh.common.attitude.good)
      .replace('{{name}}', bob.displayName)
      .replace('{{text}}', quoted + t.text.ellipsis),
  )
  expect(lines[2]).toBe(link(bobsRecommendation))
})

test('without a share sheet the text is copied', async () => {
  const { page } = carolLaptop
  await page.goto(`/p/${place}`)
  await card(page).getByRole('button', { name: t.sharePlace }).click()
  await expect(card(page).getByRole('status').filter({ hasText: t.copied })).toBeVisible()
  const copied = await page.evaluate(() => navigator.clipboard.readText())
  expect(copied.split('\n').at(-1)).toBe(link())
  expect(copied).toContain(placeName)
})

test('a shared link opened signed out leads to the recommendation after sign-in', async () => {
  const { page } = guest
  await page.goto(link(bobsRecommendation).replace(sharedInstance().baseURL, ''))
  const path = `/p/${place}?r=${bobsRecommendation}`
  await expect(page).toHaveURL(`${sharedInstance().baseURL}/login?next=${encodeURIComponent(path)}`)

  const form = zh.auth.login.password
  await page.getByLabel(form.loginName, { exact: true }).fill(carol.loginName)
  await page.getByLabel(form.password, { exact: true }).fill(carol.password)
  await page.getByRole('button', { name: form.next, exact: true }).click()
  await page.getByLabel(form.totp, { exact: true }).fill(await authenticatorOf(carol).code())
  await page.getByRole('button', { name: form.signIn, exact: true }).click()

  // Back at the card (?city= is added when the map showed another city), the recommendation highlighted and in view.
  await expect(page).toHaveURL(new RegExp(`/p/${place}\\?r=${bobsRecommendation}(&city=${city.slug})?$`))
  await expect(card(page).getByRole('heading', { level: 2 })).toHaveText(placeName)
  const rec = page.locator(`li[id="rec-${bobsRecommendation}"]`)
  await expect(rec).toHaveAttribute('data-shared', 'true')
  await expect(rec).toBeInViewport()
  await expect(page.locator('li[data-shared]')).toHaveCount(1)

  // Closing the card drops ?r=.
  await card(page).getByRole('button', { name: zh.place.close }).click()
  await expect(page).not.toHaveURL(/[?&]r=/)
})
