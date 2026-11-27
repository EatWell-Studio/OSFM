// The skeleton of M2 on the shared instance: every new path asks a signed-out phone to sign in and returns to it
// afterwards, shows its page (a placeholder until its feature is built) to a member, and the administration's sections
// are for maintainers only. Made-up data only; the place and recommendation UUIDs below exist nowhere.

import { expect, test } from '@playwright/test'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { zh } from './support/env.ts'
import {
  authenticatorOf,
  inviteMember,
  newSharedPerson,
  sharedInstance,
  sharedInstanceName,
  type TestMember,
} from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const place = '0190a1b2-0000-7000-8000-00000000b001'
const rec = '0190a1b2-0000-7000-8000-00000000c001'

let member: TestMember
let memberPhone: Person
let maintainerPhone: Person
const people: Person[] = []

test.beforeAll(async ({ browser }) => {
  member = await inviteMember()
  memberPhone = await newSharedPerson(browser, 'skeleton-member')
  maintainerPhone = await newSharedPerson(browser, 'skeleton-maintainer', sharedInstance().maintainer)
  people.push(memberPhone, maintainerPhone)
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

function url(path: string): RegExp {
  const escaped = (sharedInstance().baseURL + path).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`^${escaped}$`)
}

const pages: [string, string][] = [
  ['/new', zh.create.newPlace.title],
  [`/p/${place}`, zh.place.title],
  [`/p/${place}/edit`, zh.edit.place.title],
  [`/p/${place}/recommend`, zh.create.recommend.title],
  [`/p/${place}/r/${rec}/edit`, zh.edit.recommendation.title],
  [`/p/${place}/r/${rec}/history`, zh.edit.history.title],
  ['/list', zh.map.list.title],
]

test('a shared link asks a signed-out phone to sign in and leads back to it', async () => {
  const { page } = memberPhone
  const shared = `/p/${place}?r=${rec}`
  await page.goto(shared)
  await expect(page).toHaveURL(url(`/login?next=${encodeURIComponent(shared)}`))

  const form = zh.auth.login.password
  await page.getByLabel(form.loginName, { exact: true }).fill(member.loginName)
  await page.getByLabel(form.password, { exact: true }).fill(member.password)
  await page.getByRole('button', { name: form.next, exact: true }).click()
  await page.getByLabel(form.totp, { exact: true }).fill(await authenticatorOf(member).code())
  await page.getByRole('button', { name: form.signIn, exact: true }).click()
  await expect(page).toHaveURL(url(shared))
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(zh.place.title)
  await expect(page.getByText(zh.common.comingSoon)).toBeVisible()
})

test('every new path shows its page to a member', async () => {
  const { page } = memberPhone
  for (const [path, title] of pages) {
    await page.goto(path)
    await expect(page).toHaveURL(url(path))
    await expect(page.getByRole('heading', { level: 1 })).toHaveText(title)
  }
  // A place that is not a UUID is no address of the app.
  await page.goto('/p/not-a-place')
  await expect(page.getByRole('alert')).toHaveText(zh.common.invalidLink)
})

test('the administration is for maintainers only', async () => {
  const { page } = memberPhone
  for (const section of ['invites', 'members', 'tree', 'reports', 'settings', 'backup']) {
    await page.goto(`/admin/${section}`)
    await expect(page.getByRole('alert')).toHaveText(zh.admin.notAllowed)
  }
})

test('a maintainer goes through the sections and saves the instance name', async () => {
  const { page } = maintainerPhone
  await page.goto('/admin')
  await expect(page).toHaveURL(url('/admin/invites'))
  const nav = page.getByRole('navigation', { name: zh.admin.nav.label })
  for (const [section, heading] of [
    ['members', zh.members.title],
    ['tree', zh.members.tree.title],
    ['reports', zh.reports.title],
    ['backup', zh.backup.title],
    ['settings', zh.admin.instance.title],
  ] as const) {
    await nav.getByRole('link', { name: zh.admin.nav[section], exact: true }).click()
    await expect(page).toHaveURL(url(`/admin/${section}`))
    await expect(page.getByRole('heading', { level: 2, name: heading, exact: true })).toBeVisible()
  }
  await expect(page.getByRole('heading', { level: 2, name: zh.admin.circle.title })).toBeVisible()

  // The name is saved and still there after a reload; then set back for the other spec files.
  const renamed = `${sharedInstanceName} Renamed`
  for (const name of [renamed, sharedInstanceName]) {
    await page.getByLabel(zh.setup.name.label, { exact: true }).fill(name)
    await page.getByRole('button', { name: zh.admin.instance.save, exact: true }).click()
    await expect(page.getByText(zh.admin.instance.saved)).toBeVisible()
    await page.reload()
    await expect(page.getByLabel(zh.setup.name.label, { exact: true })).toHaveValue(name)
    await expect(page.getByRole('banner')).toContainText(name)
  }
})
