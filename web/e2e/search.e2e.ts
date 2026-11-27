// Place search on the shared instance (T039): a member searches on the map page, the browser talks only to the
// instance, the instance asks the (fake) search services, and picking a result moves the map there. When the services
// fail, the page says the pin can still be placed by hand; when maintainers turn search off, the box is gone. Made-up
// data only; the search services are the fake of support/geocoder.ts.

import { expect, type Page, test } from '@playwright/test'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { zh } from './support/env.ts'
import { failTerm, foundPlace, upstreamRequests } from './support/geocoder.ts'
import { apiAs, inviteMember, newSharedPerson, sharedInstance, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.search

let member: TestMember
let phone: Person
const people: Person[] = []

test.beforeAll(async ({ browser }) => {
  member = await inviteMember()
  phone = await newSharedPerson(browser, 'search-member', member)
  people.push(phone)
})

// eslint-disable-next-line no-empty-pattern -- Playwright needs the fixtures argument destructured
test.afterEach(async ({}, testInfo) => {
  await attachScreenshots(people, testInfo)
  // Includes every request to another origin: the browser asks the instance only.
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

// eslint-disable-next-line no-empty-pattern -- as above
test.afterAll(async ({}, testInfo) => {
  // On again, for the spec files after this one.
  await apiAs(sharedInstance().maintainer).post('/instance', { geocoderEnabled: true })
  await close(people, testInfo.project.outputDir)
})

async function openMap(page: Page) {
  await page.goto('/')
  await expect(page.locator('canvas.maplibregl-canvas')).toBeVisible()
}

/** How far, in CSS pixels, the tip of the search result's pin is from the center of the map. */
async function pinOffCenter(page: Page): Promise<number> {
  const pin = await page.getByRole('img', { name: t.marker.replace('{{name}}', foundPlace.name) }).boundingBox()
  const map = await page.locator('canvas.maplibregl-canvas').boundingBox()
  if (!pin || !map) {
    return Infinity
  }
  // The pin is anchored at its bottom center.
  return Math.hypot(pin.x + pin.width / 2 - (map.x + map.width / 2), pin.y + pin.height - (map.y + map.height / 2))
}

test('a search moves the map to the place picked, through the instance only', async () => {
  const { page } = phone
  const pageRequests: string[] = []
  page.on('request', (r) => pageRequests.push(r.url()))
  await openMap(page)

  const box = page.getByRole('searchbox', { name: t.label })
  const before = (await upstreamRequests()).length
  await box.fill('Test Harbour')
  // Nothing is sent while typing.
  await page.waitForTimeout(500)
  expect((await upstreamRequests()).length).toBe(before)

  await box.press('Enter')
  const results = page.getByRole('list', { name: t.results })
  await expect(results).toContainText(foundPlace.street)
  await results.getByRole('button', { name: new RegExp(foundPlace.name) }).click()
  await expect(results).toBeHidden()
  await expect.poll(() => pinOffCenter(page), { timeout: 10_000 }).toBeLessThan(4)

  // The browser asked the instance; the instance asked the fake, as itself.
  const searches = pageRequests.filter((u) => u.includes('/api/osfm/geocode'))
  expect(searches).toHaveLength(1)
  expect(new URL(searches[0] ?? '').origin).toBe(sharedInstance().baseURL)
  const sent = (await upstreamRequests()).slice(before)
  expect(sent).toHaveLength(1)
  expect(sent[0]?.service).toBe('photon')
  expect(sent[0]?.query.q).toBe('Test Harbour')
  expect(sent[0]?.query.bbox).toBe('7.02,0.31,7.1,0.37')
  expect(String(sent[0]?.headers['user-agent'])).toMatch(/^OSFM\/\S+ \(/)
  for (const name of ['cookie', 'x-forwarded-for', 'referer', 'origin', 'accept-language', 'x-osfm']) {
    expect(sent[0]?.headers[name], name).toBeUndefined()
  }
})

test('when the search services fail, the page says the pin can be placed by hand', async () => {
  const { page } = phone
  await openMap(page)
  const before = (await upstreamRequests()).length
  await page.getByRole('searchbox', { name: t.label }).fill(`Test ${failTerm}`)
  await page.getByRole('button', { name: t.submit, exact: true }).click()
  await expect(page.getByText(t.unavailable)).toBeVisible()
  // Both services were asked, Photon first.
  expect((await upstreamRequests()).slice(before).map((r) => r.service)).toEqual(['photon', 'nominatim'])
  // The map still works.
  await expect(page.getByRole('button', { name: zh.map.controls.zoomIn })).toBeEnabled()
  await page.getByRole('button', { name: zh.map.controls.zoomIn }).click()
})

test('with search turned off, the box is gone', async () => {
  const { page } = phone
  await apiAs(sharedInstance().maintainer).post('/instance', { geocoderEnabled: false })
  await openMap(page)
  await expect(page.getByRole('combobox', { name: zh.map.city })).toBeVisible()
  await expect(page.getByRole('search')).toHaveCount(0)
})
