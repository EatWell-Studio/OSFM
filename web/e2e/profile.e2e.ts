// The member's profile on the shared instance (/me): a member sets a pseudonym and external links in the browser and
// they are kept; another member's main name or pseudonym is refused in any letter case, with the same answer for both;
// the login name is never a name, at registration or later. Made-up data only.
//
// That other members see a pseudonym recommendation with the new pseudonym, without external links and without the
// author's uuid, is checked by the Go integration test TestProfile (cmd/osfm): no route creates recommendations yet.

import { expect, type Page, test } from '@playwright/test'
import type { Profile } from '../src/api/types.gen.ts'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { zh } from './support/env.ts'
import { ApiFailure, apiAs, inviteMember, newSharedPerson, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.profile
const suffix = Date.now().toString(36)
const pen = `Test Pen ${suffix}`

let alice: TestMember
let bob: TestMember
let alicePhone: Person
const people: Person[] = []

test.beforeAll(async ({ browser }) => {
  alice = await inviteMember({ displayName: `Test Alice ${suffix}` })
  bob = await inviteMember({ displayName: `Test Bob ${suffix}` })
  alicePhone = await newSharedPerson(browser, 'profile-alice', alice)
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

/** The profile section of the "me" page and its controls. */
function profileForm(page: Page) {
  const section = page.getByRole('region', { name: t.title })
  return {
    section,
    displayName: section.getByLabel(t.displayName, { exact: true }),
    pseudonym: section.getByLabel(t.pseudonym, { exact: true }),
    link: (n: number) => section.getByLabel(t.linkURL.replace('{{n}}', String(n)), { exact: true }),
    label: (n: number) => section.getByLabel(t.linkLabel.replace('{{n}}', String(n)), { exact: true }),
    addLink: section.getByRole('button', { name: t.addLink, exact: true }),
    save: section.getByRole('button', { name: t.save, exact: true }),
  }
}

async function failure(promise: Promise<unknown>): Promise<ApiFailure> {
  try {
    await promise
  } catch (e) {
    if (e instanceof ApiFailure) {
      return e
    }
    throw e
  }
  throw new Error('e2e: the request was not refused')
}

test('a member sets a pseudonym and an external link, and they are kept', async () => {
  const { page } = alicePhone
  await page.goto('/me')
  const form = profileForm(page)
  await expect(form.displayName).toHaveValue(alice.displayName)
  await expect(form.pseudonym).toHaveValue('')

  await form.pseudonym.fill(pen)
  await form.addLink.click()
  await form.link(1).fill('https://example.test/test-alice')
  await form.label(1).fill('Test Blog')
  await form.save.click()
  await expect(form.section.getByText(t.saved)).toBeVisible()

  await page.reload()
  await expect(form.pseudonym).toHaveValue(pen)
  await expect(form.link(1)).toHaveValue('https://example.test/test-alice')
  await expect(form.label(1)).toHaveValue('Test Blog')
  const saved = (await apiAs(alice).get('/me/profile')) as Profile
  expect(saved).toMatchObject({
    displayName: alice.displayName,
    pseudonym: pen,
    externalLinks: [{ url: 'https://example.test/test-alice', label: 'Test Blog' }],
    pseudonymInUse: false,
  })
})

test('another member’s name is refused in any letter case, without saying whose or which kind', async () => {
  const { page } = alicePhone
  await page.goto('/me')
  const form = profileForm(page)
  await form.displayName.fill(bob.displayName.toUpperCase())
  await form.save.click()
  await expect(form.section.getByText(t.errors.nameTaken)).toBeVisible()
  await expect(form.displayName).toHaveAttribute('aria-invalid', 'true')

  // Bob asks for Alice's pseudonym and for her main name: the same answer.
  const asPseudonym = await failure(apiAs(bob).post('/me/profile', { displayName: pen.toLowerCase() }))
  const asMainName = await failure(apiAs(bob).post('/me/profile', { displayName: alice.displayName.toLowerCase() }))
  expect([asPseudonym.status, asPseudonym.code]).toEqual([409, 'name_taken'])
  expect([asMainName.status, asMainName.code]).toEqual([409, 'name_taken'])
  // The login name is not a name either.
  const login = await failure(apiAs(bob).post('/me/profile', { pseudonym: bob.loginName.toUpperCase() }))
  expect([login.status, login.code]).toEqual([400, 'name_is_login_name'])
})

test('registration refuses an empty display name, the login name and another member’s name', async () => {
  for (const [who, code] of [
    [{ displayName: '' }, 'display_name_invalid'],
    [{ loginName: `test_login_${suffix}`, displayName: `TEST_LOGIN_${suffix}` }, 'display_name_is_login_name'],
    [{ displayName: pen.toUpperCase() }, 'name_taken'],
  ] as const) {
    const refused = await failure(inviteMember(who))
    expect(refused.code).toBe(code)
  }
})
