// Member invites and the cooling-off period on the shared instance (D12): a new member sees on the "me" page how many
// days are left before they can invite, the maintainer opens inviting early from the member list, the member creates
// an invite link, and the person it is for registers with it, as invited by the member. Made-up data only.

import { expect, type Page, test } from '@playwright/test'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { zh } from './support/env.ts'
import { AuthenticatorApp } from './support/totp.ts'
import { apiAs, inviteMember, newSharedPerson, sharedInstance, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

let member: TestMember
let memberPhone: Person
let maintainerPhone: Person
let newcomerPhone: Person
const people: Person[] = []
let link = ''

test.beforeAll(async ({ browser }) => {
  member = await inviteMember()
  memberPhone = await newSharedPerson(browser, 'invite-member', member)
  maintainerPhone = await newSharedPerson(browser, 'invite-maintainer', sharedInstance().maintainer)
  newcomerPhone = await newSharedPerson(browser, 'invite-newcomer')
  people.push(memberPhone, maintainerPhone, newcomerPhone)
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

const invites = zh.invites

function text(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(values[name]))
}

function invitesSection(page: Page) {
  return page.getByRole('region', { name: invites.title, exact: true })
}

test('a new member sees the days left of the cooling-off period', async () => {
  const { page } = memberPhone
  await page.goto('/me')
  const section = invitesSection(page)
  // A new instance's period is 30 days, from joining a moment ago.
  const days = text(invites.me.cooldown_other.split('{{date}}')[0] ?? '', { count: 30 })
  await expect(section.getByText(days)).toBeVisible()
  await expect(section.getByRole('button', { name: invites.create, exact: true })).toBeDisabled()
  await expect(apiAs(member).post('/invites', { days: 7 })).rejects.toMatchObject({
    status: 403,
    code: 'invite_not_allowed',
  })
})

test('the maintainer opens inviting early from the member list', async () => {
  const { page } = maintainerPhone
  await page.goto('/admin/members')
  const row = page
    .getByRole('list', { name: zh.members.title, exact: true })
    .getByRole('listitem')
    .filter({ hasText: member.displayName })
  await expect(row.getByRole('checkbox', { name: zh.members.invites.permission, exact: true })).toBeChecked()
  await row.getByRole('button', { name: zh.members.invites.openEarly, exact: true }).click()
  await expect(row.getByRole('button', { name: zh.members.invites.openEarly, exact: true })).toBeHidden()
})

test('the member creates an invite link', async () => {
  const { page } = memberPhone
  await page.reload()
  const section = invitesSection(page)
  await expect(section.getByText(text(invites.me.allowed_other, { count: 3, active: 0 }))).toBeVisible()
  await section.getByRole('button', { name: invites.create, exact: true }).click()
  const field = section.getByLabel(invites.link, { exact: true })
  await expect(field).toHaveValue(/\/register#invite=/)
  link = await field.inputValue()
  // The member's list has their invite only.
  await expect(section.getByRole('list', { name: invites.title }).getByRole('listitem')).toHaveCount(1)
  await expect(section.getByText(text(invites.me.allowed_other, { count: 3, active: 1 }))).toBeVisible()
})

test('the person invited registers, as invited by the member', async () => {
  const { page } = newcomerPhone
  await page.goto(link)
  await expect(page.getByText(zh.auth.register.validInvite)).toBeVisible()
  const form = zh.auth.register.password
  const suffix = Date.now().toString(36)
  const displayName = `Test Newcomer ${suffix}`
  const password = 'test password, made up'
  await page.getByLabel(form.loginName, { exact: true }).fill(`test_newcomer_${suffix}`)
  await page.getByLabel(form.displayName, { exact: true }).fill(displayName)
  await page.getByLabel(form.password, { exact: true }).fill(password)
  await page.getByLabel(form.repeat, { exact: true }).fill(password)
  await page.getByRole('button', { name: form.next, exact: true }).click()
  await expect(page.getByRole('img', { name: form.qrTitle })).toBeVisible()
  const app = new AuthenticatorApp(await page.locator('code').innerText())
  await page.getByLabel(form.code, { exact: true }).fill(await app.code())
  await page.getByRole('button', { name: form.confirm, exact: true }).click()
  const codes = page.getByRole('list', { name: zh.auth.recoveryCodes.title })
  await expect(codes.getByRole('listitem')).toHaveCount(10)
  await page.getByLabel(zh.auth.recoveryCodes.confirm).check()
  await page.getByRole('button', { name: zh.auth.recoveryCodes.continue, exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`^${sharedInstance().baseURL.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}/$`))

  // The maintainer's member list names the member as the inviter.
  const list = (await apiAs(sharedInstance().maintainer).get('/members')) as {
    members: { displayName: string; invitedBy: { uuid: string } | null }[]
  }
  const newcomer = list.members.find((m) => m.displayName === displayName)
  expect(newcomer?.invitedBy?.uuid).toBe(member.uuid)

  // The member sees their invite as used.
  await memberPhone.page.reload()
  await expect(
    invitesSection(memberPhone.page)
      .getByRole('list', { name: invites.title })
      .getByRole('listitem')
      .filter({ hasText: invites.state.used }),
  ).toHaveCount(1)
})
