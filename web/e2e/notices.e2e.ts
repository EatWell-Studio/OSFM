// The member's notices on the shared instance (M2-Q14): the unread count on "me" in the bottom navigation, the notices
// at the top of the "me" page with a text per kind, a link to the recommendation while it exists and a generic text
// once it is gone; reading lowers the count; another member's notice cannot be marked. Made-up data only.
//
// No route writes notices yet (the edits and reports that do are later tasks), so the harness writes them into the
// instance's database, as an operator could (as support/session.ts does for sessions), in the shape domain.Notify
// stores them. The Go integration test TestNotices (cmd/osfm) covers the same with notices written by domain.Notify.

import { randomBytes } from 'node:crypto'
import { join } from 'node:path'
import { DatabaseSync } from 'node:sqlite'
import { expect, type Page, test } from '@playwright/test'
import type { NoticeList } from '../src/api/types.gen.ts'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { city, zh } from './support/env.ts'
import {
  ApiFailure,
  apiAs,
  createPlace,
  inviteMember,
  newSharedPerson,
  sharedInstance,
  type TestMember,
} from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.notices
const suffix = Date.now().toString(36)
const placeName = `Test Notice Noodles ${suffix}`

let alice: TestMember
let bob: TestMember
let alicePhone: Person
let created: { place: string; recommendation: string }
const people: Person[] = []

/** A UUIDv7 as the server writes them: the time in milliseconds, then random bits. */
function uuidV7(ms: number): string {
  const bytes = randomBytes(16)
  bytes.writeUIntBE(ms, 0, 6)
  bytes[6] = 0x70 | ((bytes[6] ?? 0) & 0x0f)
  bytes[8] = 0x80 | ((bytes[8] ?? 0) & 0x3f)
  const hex = bytes.toString('hex')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

/** A record ID as PocketBase makes them: 15 lowercase letters and digits. */
function recordID(): string {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789'
  return Array.from(randomBytes(15), (b) => alphabet[b % alphabet.length]).join('')
}

let clock = Date.now() - 60_000

/** Writes an unread notice of kind about the recommendation ref for the member, a second newer than the last one. */
function writeNotice(member: TestMember, kind: string, ref: string): string {
  clock += 1000
  const uuid = uuidV7(clock)
  const db = new DatabaseSync(join(sharedInstance().dataDir, 'pb_data', 'data.db'))
  try {
    db.exec('PRAGMA busy_timeout = 5000')
    const row = db.prepare('SELECT id FROM members WHERE uuid = ?').get(member.uuid) as { id: string } | undefined
    if (!row) {
      throw new Error('e2e: no such member')
    }
    db.prepare("INSERT INTO notices (id, member, kind, ref, read_at, created, uuid) VALUES (?, ?, ?, ?, '', ?, ?)").run(
      recordID(),
      row.id,
      kind,
      ref,
      new Date(clock).toISOString().replace('T', ' '),
      uuid,
    )
  } finally {
    db.close()
  }
  return uuid
}

test.beforeAll(async ({ browser }) => {
  alice = await inviteMember({ displayName: `Test Alice ${suffix}` })
  bob = await inviteMember({ displayName: `Test Bob ${suffix}` })
  const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
  created = await createPlace(alice, {
    name: placeName,
    lat: (minLat + maxLat) / 2 + 0.003,
    lon: (minLon + maxLon) / 2 + 0.003,
    recommendation: { attitude: 'good', text: 'Test notice text, made up' },
  })
  alicePhone = await newSharedPerson(browser, 'notices-alice', alice)
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

function badge(page: Page) {
  return page.getByRole('navigation', { name: zh.common.nav.label }).getByTestId('unread-notices')
}

function section(page: Page) {
  return page.getByRole('region', { name: t.title })
}

const withPlace = (text: string) => text.replace('{{place}}', placeName)

let edited: string

test('a member sees the unread count and the notices, and reads them', async () => {
  writeNotice(alice, 'hidden_by_reports', created.recommendation)
  edited = writeNotice(alice, 'maintainer_edited', created.recommendation)
  // Deleted as a violation: the recommendation it names does not exist (any more).
  writeNotice(alice, 'violation_deleted', uuidV7(clock))

  const { page } = alicePhone
  await page.goto('/')
  await expect(badge(page)).toHaveText('3')
  await page
    .getByRole('navigation', { name: zh.common.nav.label })
    .getByRole('link', { name: zh.common.nav.me })
    .click()

  const notices = section(page)
  const items = notices.getByRole('listitem')
  await expect(items).toHaveCount(3)
  // Newest first; no name of anyone in any of them.
  await expect(items.nth(0)).toContainText(t.kind.violation_deleted.gone)
  await expect(items.nth(1)).toContainText(withPlace(t.kind.maintainer_edited.target))
  await expect(items.nth(2)).toContainText(withPlace(t.kind.hidden_by_reports.target))
  await expect(notices).not.toContainText(sharedInstance().maintainer.displayName)

  // The deleted one has no link, only a button.
  await expect(items.nth(0).getByRole('link')).toHaveCount(0)
  await items.nth(0).getByRole('button', { name: t.markRead }).click()
  await expect(badge(page)).toHaveText('2')

  // The edit opens the revisions of the recommendation and is read.
  await items.nth(1).getByRole('link').click()
  await expect(page).toHaveURL(`/p/${created.place}/r/${created.recommendation}/history`)
  await expect(badge(page)).toHaveText('1')

  // The report notice links to the card.
  await page.goBack()
  await expect(section(page).getByRole('listitem').nth(2).getByRole('link')).toHaveAttribute(
    'href',
    `/p/${created.place}?r=${created.recommendation}`,
  )
  await section(page).getByRole('button', { name: t.markAllRead }).click()
  await expect(badge(page)).toHaveCount(0)
  await page.reload()
  await expect(section(page).getByRole('listitem')).toHaveCount(3)
  await expect(badge(page)).toHaveCount(0)
})

test("another member's notice cannot be marked read, and stays unread", async () => {
  const bobs = writeNotice(bob, 'restored', created.recommendation)
  for (const uuids of [[bobs], [edited, bobs]]) {
    const refused = await apiAs(alice)
      .post('/me/notices/read', { uuids })
      .then(
        () => null,
        (e: unknown) => e,
      )
    expect(refused).toBeInstanceOf(ApiFailure)
    expect((refused as ApiFailure).status).toBe(404)
  }
  const list = (await apiAs(bob).get('/me/notices')) as NoticeList
  expect(list.unread).toBe(1)
  // The notice names Alice's recommendation, not one of Bob's: no target.
  expect(list.notices[0]?.target).toBeNull()
})
