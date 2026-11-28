// The per-layer GeoJSON exports on the shared instance (/me): a member downloads both layers in the browser; the files
// are GeoJSON FeatureCollections with their format version and licence, hold a circle recommendation and its place,
// and neither another member's private recommendation nor the place it alone is on. What exactly is in the files,
// as the four identities, is checked by the Go integration tests (cmd/osfm/layer_export_test.go). Made-up data only.

import { readFile } from 'node:fs/promises'
import { expect, type Page, test } from '@playwright/test'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { city, zh } from './support/env.ts'
import { createPlace, inviteMember, newSharedPerson, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.layerExport
const suffix = Date.now().toString(36)
const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// Points in the city, apart from where other specs put their places.
const at = (x: number, y: number) => ({ lon: minLon + (maxLon - minLon) * x, lat: minLat + (maxLat - minLat) * y })

let alice: TestMember
let bob: TestMember
let alicePhone: Person
const people: Person[] = []
let shared: { place: string; recommendation: string }
let hidden: { place: string; recommendation: string }

test.beforeAll(async ({ browser }) => {
  alice = await inviteMember({ displayName: `Test Export Alice ${suffix}` })
  bob = await inviteMember({ displayName: `Test Export Bob ${suffix}` })
  shared = await createPlace(bob, {
    name: `Test Export Shared ${suffix}`,
    ...at(0.37, 0.61),
    recommendation: { attitude: 'good', text: 'Test export shared text' },
  })
  hidden = await createPlace(bob, {
    name: `Test Export Private ${suffix}`,
    ...at(0.39, 0.63),
    recommendation: { attitude: 'ok', text: 'Test export private text', visibility: 'private' },
  })
  alicePhone = await newSharedPerson(browser, 'layer-export-alice', alice)
  people.push(alicePhone)
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

interface Collection {
  type: string
  'osfm:schema': string
  'osfm:license': { id: string }
  'osfm:attribution': string
  features: { id: string; geometry: { type: string; coordinates: number[] }; properties: Record<string, unknown> }[]
}

/** Clicks the download button and returns the downloaded file's name and parsed content. */
async function download(page: Page, button: string): Promise<{ name: string; file: Collection }> {
  const section = page.getByRole('region', { name: t.title })
  const [file] = await Promise.all([
    page.waitForEvent('download'),
    section.getByRole('button', { name: button }).click(),
  ])
  const path = await file.path()
  return { name: file.suggestedFilename(), file: JSON.parse(await readFile(path, 'utf8')) as Collection }
}

test('a member downloads both layers from the me page', async () => {
  const page = alicePhone.page
  await page.goto('/me')
  await expect(page.getByRole('region', { name: t.title }).getByText(t.places.license)).toBeVisible()

  const places = await download(page, t.places.download)
  expect(places.name).toMatch(/^osfm-places-\d{4}-\d{2}-\d{2}\.geojson$/)
  expect(places.file.type).toBe('FeatureCollection')
  expect(places.file['osfm:schema']).toBe('osfm-places/1')
  expect(places.file['osfm:license'].id).toBe('ODbL-1.0')
  expect(places.file['osfm:attribution']).toBe('© OpenStreetMap contributors')
  const placeIDs = places.file.features.map((f) => f.id)
  expect(placeIDs).toContain(shared.place)
  expect(placeIDs).not.toContain(hidden.place)
  const place = places.file.features.find((f) => f.id === shared.place)
  expect(place?.geometry.type).toBe('Point')
  const where = at(0.37, 0.61)
  expect(place?.geometry.coordinates[0]).toBeCloseTo(where.lon, 4)
  expect(place?.geometry.coordinates[1]).toBeCloseTo(where.lat, 4)
  await expect(page.getByRole('region', { name: t.title }).getByRole('status')).toContainText(places.name)

  const recs = await download(page, t.recommendations.download)
  expect(recs.name).toMatch(/^osfm-recommendations-\d{4}-\d{2}-\d{2}\.geojson$/)
  expect(recs.file['osfm:schema']).toBe('osfm-recommendations/1')
  expect(recs.file['osfm:license'].id).toBe('CC-BY-SA-4.0')
  const rec = recs.file.features.find((f) => f.id === shared.recommendation)
  expect(rec?.properties['osfm:display_name']).toBe(`Test Export Bob ${suffix}`)
  expect(rec?.properties['osfm:place']).toBe(shared.place)
  expect(recs.file.features.map((f) => f.id)).not.toContain(hidden.recommendation)
  expect(JSON.stringify(recs.file)).not.toContain('Test export private text')
})
