// The invite tree on the shared instance (D7, D10): the maintainer sees a branch of three members who invited each
// other, previews stopping its invites, re-verifies when asked and stops them; then removes another member from the
// member list, hiding their recommendations: the removed member's session ends and members no longer see the
// recommendation, while maintainers see it as hidden. Made-up data only.

import { expect, type Page, test } from '@playwright/test'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { city, zh } from './support/env.ts'
import { makeSessionStale } from './support/session.ts'
import { AuthenticatorApp } from './support/totp.ts'
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

const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// A point in the city, apart from where other specs put their places.
const lat = minLat + (maxLat - minLat) * 0.61
const lon = minLon + (maxLon - minLon) * 0.17

let root: TestMember
let child: TestMember
let grandchild: TestMember
let removed: TestMember
let place = ''
let maintainerPhone: Person
const people: Person[] = []

test.beforeAll(async ({ browser }) => {
  const maintainer = apiAs(sharedInstance().maintainer)
  // root → child → grandchild: each opened early by the maintainer so that they can invite at once.
  root = await inviteMember()
  await maintainer.post(`/members/${root.uuid}/invite-early`)
  child = await inviteMember({ inviter: root })
  await maintainer.post(`/members/${child.uuid}/invite-early`)
  grandchild = await inviteMember({ inviter: child })
  removed = await inviteMember()
  place = (
    await createPlace(removed, {
      name: 'Test Tree Dumplings',
      lat,
      lon,
      recommendation: { attitude: 'good', text: 'Test text, made up' },
    })
  ).place
  maintainerPhone = await newSharedPerson(browser, 'tree-maintainer', sharedInstance().maintainer)
  people.push(maintainerPhone)
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

const t = zh.members

function count(template: string, n: number): string {
  return template.replace('{{count}}', String(n))
}

/** Fills in the verification panel with the shared maintainer's password and a code of a time step nobody used. */
async function verify(page: Page) {
  const maintainer = sharedInstance().maintainer
  const panel = page.getByRole('region', { name: zh.auth.credentials.verify.title })
  await expect(panel).toBeVisible()
  await panel.getByLabel(zh.auth.login.password.password, { exact: true }).fill(maintainer.password)
  // Other spec files may have used the current step: wait for the next one.
  const app = new AuthenticatorApp(maintainer.totpSecret, Math.floor(Date.now() / 30_000))
  await panel.getByLabel(zh.auth.credentials.verify.code, { exact: true }).fill(await app.code())
  await panel.getByRole('button', { name: zh.auth.credentials.verify.submit, exact: true }).click()
  await expect(panel).toBeHidden()
}

test('the maintainer previews a branch, re-verifies and stops its invites', async () => {
  const { page, context } = maintainerPhone
  // Ten minutes after the last verification: the page asks for one before acting.
  await makeSessionStale(context, sharedInstance().dataDir)
  await page.goto('/admin/tree')
  const rootRow = page.getByRole('group', { name: root.displayName, exact: true })
  await expect(rootRow).toBeVisible()
  const below = page.getByRole('list', { name: t.tree.invitedBy.replace('{{name}}', root.displayName), exact: true })
  await expect(below.getByRole('group', { name: child.displayName, exact: true })).toBeVisible()
  await expect(below.getByRole('group', { name: grandchild.displayName, exact: true })).toBeVisible()
  await expect(page.locator('main')).not.toContainText(root.loginName)

  await rootRow.getByRole('button', { name: t.tree.stopInvites, exact: true }).click()
  const affected = page.getByRole('list', { name: t.tree.preview.affectedList, exact: true })
  await expect(affected.getByRole('listitem')).toHaveText([root.displayName, child.displayName, grandchild.displayName])
  await page.getByRole('button', { name: count(t.tree.preview.confirmStop_other, 3), exact: true }).click()
  await verify(page)
  await expect(
    page.getByText(count(t.tree.stopped_other, 3).replace('{{name}}', root.displayName), { exact: true }),
  ).toBeVisible()

  const list = (await apiAs(sharedInstance().maintainer).get('/members')) as {
    members: { uuid: string; canInvite: boolean }[]
  }
  for (const m of [root, child, grandchild]) {
    expect(list.members.find((x) => x.uuid === m.uuid)?.canInvite).toBe(false)
  }
  await expect(apiAs(child).post('/invites', { days: 1 })).rejects.toMatchObject({
    status: 403,
    code: 'invite_not_allowed',
  })
})

test('the maintainer removes a member and hides their recommendations', async () => {
  const { page } = maintainerPhone
  // The place is visible to other members before.
  await apiAs(root).get(`/places/${place}`)

  await page.goto('/admin/members')
  const row = page
    .getByRole('list', { name: t.title, exact: true })
    .getByRole('listitem')
    .filter({ hasText: removed.displayName })
  await row.getByRole('button', { name: t.remove.action, exact: true }).click()
  await row.getByRole('radio', { name: new RegExp(`^${t.remove.hide}`) }).check()
  await row.getByLabel(t.remove.reason, { exact: true }).fill('Test reason, made up')
  await row.getByRole('button', { name: t.remove.confirm, exact: true }).click()
  await expect(page.getByText(t.remove.done.replace('{{name}}', removed.displayName), { exact: true })).toBeVisible()
  await expect(
    page
      .getByRole('list', { name: t.left, exact: true })
      .getByRole('listitem')
      .filter({ hasText: removed.displayName }),
  ).toBeVisible()

  // The removed member's session has ended; members no longer see the place, maintainers see the hidden recommendation.
  const ended = await apiAs(removed)
    .get('/me')
    .then(() => null)
    .catch((e: unknown) => e)
  expect(ended).toBeInstanceOf(ApiFailure)
  expect((ended as ApiFailure).status).toBe(401)
  await expect(apiAs(root).get(`/places/${place}`)).rejects.toMatchObject({ status: 404 })
  const card = (await apiAs(sharedInstance().maintainer).get(`/places/${place}`)) as {
    recommendations: { hidden?: { kind: string; reason?: string } }[]
  }
  expect(card.recommendations.map((r) => r.hidden)).toEqual([
    { kind: 'member_removed', reason: 'Test reason, made up' },
  ])
})
