// The personal data export on the shared instance (/me): a member downloads the file in the browser, and it is the
// osfm-personal/1 JSON with their account and their private and pseudonym recommendations, without their password,
// TOTP secret, session or recovery codes and without another member's name or uuid. The contents in detail are checked
// by the Go integration tests (internal/api, cmd/osfm). Made-up data only.

import { readFile } from 'node:fs/promises'
import { expect, test } from '@playwright/test'
import type { PersonalExport } from '../src/api/types.gen.ts'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { city, zh } from './support/env.ts'
import { apiAs, createPlace, inviteMember, newSharedPerson, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.personalExport
const suffix = Date.now().toString(36)
const pen = `Test Export Pen ${suffix}`
const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// Points in the city, apart from where other specs put their places.
const at = (x: number, y: number) => ({ lat: minLat + (maxLat - minLat) * y, lon: minLon + (maxLon - minLon) * x })

let alice: TestMember
let bob: TestMember
let alicePhone: Person
const people: Person[] = []
let privateRec = ''
let penRec = ''

test.beforeAll(async ({ browser }) => {
  alice = await inviteMember({ displayName: `Test Export Alice ${suffix}` })
  bob = await inviteMember({ displayName: `Test Export Bob ${suffix}` })
  await apiAs(alice).post('/me/profile', { pseudonym: pen })
  privateRec = (
    await createPlace(alice, {
      name: `Test Export Bakery ${suffix}`,
      ...at(0.37, 0.61),
      recommendation: { attitude: 'good', text: 'Test private export text', visibility: 'private' },
    })
  ).recommendation
  penRec = (
    await createPlace(alice, {
      name: `Test Export Noodles ${suffix}`,
      ...at(0.39, 0.63),
      recommendation: { attitude: 'must_try', text: 'Test pen name export text', displayAs: 'pseudonym' },
    })
  ).recommendation
  // Bob's recommendation of a place of his own: nothing of it belongs in Alice's export.
  await createPlace(bob, {
    name: `Test Export Tea ${suffix}`,
    ...at(0.41, 0.65),
    recommendation: { attitude: 'ok', text: 'Test bob export text' },
  })
  alicePhone = await newSharedPerson(browser, 'export-alice', alice)
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

test('a member downloads their own data as osfm-personal/1 JSON', async () => {
  const { page } = alicePhone
  await page.goto('/me')
  const section = page.getByRole('region', { name: t.title })
  await expect(section.getByText(t.contents.recommendations)).toBeVisible()

  const [download] = await Promise.all([
    page.waitForEvent('download'),
    section.getByRole('button', { name: t.download, exact: true }).click(),
  ])
  expect(download.suggestedFilename()).toMatch(/^osfm-personal-export-\d{4}-\d{2}-\d{2}\.json$/)
  const text = await readFile(await download.path(), 'utf8')
  const data = JSON.parse(text) as PersonalExport

  expect(data['osfm:schema']).toBe('osfm-personal/1')
  expect(data.account).toMatchObject({
    uuid: alice.uuid,
    loginName: alice.loginName,
    displayName: alice.displayName,
    pseudonym: pen,
    role: 'member',
  })
  expect(data.account.invitedBy).not.toBe('')
  expect(data.credentials.map((c) => c.kind)).toEqual(['password_totp'])
  const recs = new Map(data.recommendations.map((r) => [r.uuid, r]))
  expect(recs.get(privateRec)).toMatchObject({ visibility: 'private', text: 'Test private export text' })
  expect(recs.get(penRec)).toMatchObject({ displayAs: 'pseudonym', text: 'Test pen name export text' })
  expect(recs.size).toBe(2)

  for (const secret of [alice.password, alice.totpSecret, alice.session, ...alice.recoveryCodes]) {
    expect(text, 'a secret in the export').not.toContain(secret)
  }
  for (const other of [bob.uuid, bob.loginName, bob.displayName, 'Test bob export text', `Test Export Tea ${suffix}`]) {
    expect(text, "another member's data in the export").not.toContain(other)
  }
  await expect(section.getByRole('alert')).toHaveCount(0)
})
