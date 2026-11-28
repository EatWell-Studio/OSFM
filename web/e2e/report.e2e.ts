// Reports and their review on the shared instance (C6, D11): three members report a recommendation from the place
// card; at the threshold of three it is hidden and disappears for members (its author is told, without who reported);
// the maintainer restores it in the review at /admin/reports and it appears again. Made-up data only.

import { expect, type Page, test } from '@playwright/test'
import type { NoticeList, PlaceCard } from '../src/api/types.gen.ts'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { city, zh } from './support/env.ts'
import { apiAs, createPlace, inviteMember, newSharedPerson, sharedInstance, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.reports
const suffix = Date.now().toString(36)
const placeName = `Test Report Noodles ${suffix}`
const reportedText = `Test reported text ${suffix}, made up`
const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// A point in the city, apart from where other specs put their places.
const lat = minLat + (maxLat - minLat) * 0.27
const lon = minLon + (maxLon - minLon) * 0.71

let author: TestMember
let neighbour: TestMember
let reporters: TestMember[] = []
let created: { place: string; recommendation: string }
let thresholdBefore = 3
const phones: Person[] = []
let maintainerPhone: Person

async function maintainerSettings(): Promise<{ reportThreshold: number }> {
  return (await apiAs(sharedInstance().maintainer).get('/instance')) as { reportThreshold: number }
}

test.beforeAll(async ({ browser }) => {
  // Another spec may have changed the threshold of the shared instance: three, as the flow says, and back afterwards.
  thresholdBefore = (await maintainerSettings()).reportThreshold
  await apiAs(sharedInstance().maintainer).post('/instance', { reportThreshold: 3 })

  author = await inviteMember()
  neighbour = await inviteMember()
  reporters = [await inviteMember(), await inviteMember(), await inviteMember()]
  created = await createPlace(author, {
    name: placeName,
    lat,
    lon,
    recommendation: { attitude: 'good', text: reportedText },
  })
  // A second recommendation keeps the place on the map while the first is hidden.
  await apiAs(neighbour).post(`/places/${created.place}/recommendations`, {
    attitude: 'ok',
    text: 'Test neighbour text, made up',
  })
  for (const [i, reporter] of reporters.entries()) {
    phones.push(await newSharedPerson(browser, `report-member-${String(i)}`, reporter))
  }
  maintainerPhone = await newSharedPerson(browser, 'report-maintainer', sharedInstance().maintainer)
})

// eslint-disable-next-line no-empty-pattern -- Playwright needs the fixtures argument destructured
test.afterEach(async ({}, testInfo) => {
  const people = [...phones, maintainerPhone]
  await attachScreenshots(people, testInfo)
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

// eslint-disable-next-line no-empty-pattern -- as above
test.afterAll(async ({}, testInfo) => {
  await apiAs(sharedInstance().maintainer).post('/instance', { reportThreshold: thresholdBefore })
  await close([...phones, maintainerPhone], testInfo.project.outputDir)
})

/** The recommendation on the card of the place, as the page shows it. */
function reportedOnCard(page: Page) {
  return page.locator(`#rec-${created.recommendation}`)
}

async function cardAs(member: TestMember): Promise<PlaceCard> {
  return (await apiAs(member).get(`/places/${created.place}`)) as PlaceCard
}

test('three members report the recommendation from the card and it disappears', async () => {
  for (const [i, { page }] of phones.entries()) {
    await page.goto(`/p/${created.place}`)
    const rec = reportedOnCard(page)
    await expect(rec).toContainText(reportedText)
    await rec.getByRole('button', { name: t.report.action, exact: true }).click()
    const form = rec.getByRole('form', { name: t.report.title })
    await form.getByRole('radio', { name: t.reason.false_or_duplicate, exact: true }).check()
    await form.getByLabel(t.report.note, { exact: true }).fill(`Test note ${String(i)}, made up`)
    await form.getByRole('button', { name: t.report.submit, exact: true }).click()
    if (i < 2) {
      await expect(rec.getByText(t.report.done, { exact: true })).toBeVisible()
      // Below the threshold it is still shown to the other members.
      expect((await cardAs(neighbour)).recommendations.map((r) => r.uuid)).toContain(created.recommendation)
    } else {
      // The third report hides it: the reporter's card reloads without it, the place stays for the other one.
      await expect(rec).toHaveCount(0)
      await expect(page.getByRole('heading', { name: placeName, exact: true })).toBeVisible()
    }
  }

  // Gone for members: the card and the counts; the author and the maintainer see it, hidden by reports.
  const card = await cardAs(neighbour)
  expect(card.recommendations.map((r) => r.uuid)).not.toContain(created.recommendation)
  expect(card.counts).toMatchObject({ good: 0, ok: 1 })
  for (const who of [author, sharedInstance().maintainer]) {
    const seen = (await cardAs(who)).recommendations.find((r) => r.uuid === created.recommendation)
    expect(seen?.hidden?.kind).toBe('reports')
  }
  // The author is told, with no reporter in it.
  const notices = (await apiAs(author).get('/me/notices')) as NoticeList
  expect(notices.notices.map((n) => n.kind)).toContain('hidden_by_reports')
  const body = JSON.stringify(notices)
  for (const reporter of reporters) {
    expect(body).not.toContain(reporter.uuid)
    expect(body).not.toContain(reporter.displayName)
  }
})

test('the maintainer restores it in the review and it appears again', async () => {
  const { page } = maintainerPhone
  await page.goto('/admin/reports')
  const item = page
    .getByRole('list', { name: t.queue.label, exact: true })
    .getByRole('listitem', { name: placeName, exact: true })
  await expect(item).toContainText(reportedText)
  await expect(item.getByText(t.queue.openReports_other.replace('{{count}}', '3'), { exact: true })).toBeVisible()
  await expect(item).toContainText('Test note 2, made up')
  // No reporter on the review page.
  for (const reporter of reporters) {
    await expect(page.locator('main')).not.toContainText(reporter.displayName)
  }
  await item.getByRole('button', { name: t.queue.restore, exact: true }).click()
  await expect(page.getByText(t.queue.done.restored, { exact: true })).toBeVisible()
  await expect(item).toHaveCount(0)

  // Shown again, on a member's card in the browser too.
  expect((await cardAs(neighbour)).recommendations.map((r) => r.uuid)).toContain(created.recommendation)
  const memberPage = phones[0]?.page
  if (!memberPage) {
    throw new Error('no member phone')
  }
  await memberPage.goto(`/p/${created.place}`)
  await expect(reportedOnCard(memberPage)).toContainText(reportedText)
  const notices = (await apiAs(author).get('/me/notices')) as NoticeList
  expect(notices.notices.map((n) => n.kind)).toContain('restored')
})
