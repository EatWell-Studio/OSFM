// A circle from its first start: the operator sets up the instance, the maintainer invites two members, one registers
// with password and TOTP and one with a passkey, both sign out and in again and look at the map, and a session ended
// on the server sends its member back to the sign-in page. Made-up data only.
//
// All requests come from 127.0.0.1, so the flows share one client's authentication rate limit (a burst of 30 requests
// to the sign-in and registration routes, docs/architecture.md §3.3); they make about 15 such requests and no failed
// attempts.

import { devices, expect, type Page, request, test } from '@playwright/test'
import {
  addVirtualAuthenticator,
  attachScreenshots,
  close,
  newPerson,
  type Person,
  takeProblems,
} from './support/browser.ts'
import { baseURL, city, instanceName, setupCode, zh } from './support/env.ts'
import { AuthenticatorApp } from './support/totp.ts'

test.describe.configure({ mode: 'serial' })

const phone = devices['Pixel 7']
const password = 'test password, made up'
const tileRequest = new RegExp(`/tiles/${city.slug}/\\d+/\\d+/\\d+\\.mvt$`)

let maintainer: Person
let alice: Person // password and TOTP
let bob: Person // passkey
let people: Person[] = []
let aliceApp: AuthenticatorApp
const invites: string[] = []

test.beforeAll(async ({ browser }) => {
  maintainer = await newPerson(browser, 'maintainer', phone)
  alice = await newPerson(browser, 'alice', phone)
  bob = await newPerson(browser, 'bob', phone)
  people = [maintainer, alice, bob]
  // Bob's phone has a passkey provider for the whole test: its credentials live as long as the page.
  await addVirtualAuthenticator(bob.page)
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
  const escaped = (baseURL() + path).replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`^${escaped}$`)
}

/** Fills the password form of registration (or setup) and confirms it with a code from the authenticator app. */
async function registerWithPassword(page: Page, loginName: string, displayName: string): Promise<AuthenticatorApp> {
  const form = zh.pages.register.password
  await page.getByLabel(form.loginName, { exact: true }).fill(loginName)
  await page.getByLabel(form.displayName, { exact: true }).fill(displayName)
  await page.getByLabel(form.password, { exact: true }).fill(password)
  await page.getByLabel(form.repeat, { exact: true }).fill(password)
  await page.getByRole('button', { name: form.next, exact: true }).click()
  await expect(page.getByRole('img', { name: form.qrTitle })).toBeVisible()
  // No session before the second factor.
  expect((await page.context().cookies()).map((c) => c.name)).not.toContain('osfm_session')
  // The secret as the page shows it for typing into an app.
  const app = new AuthenticatorApp(await page.locator('code').innerText())
  await page.getByLabel(form.code, { exact: true }).fill(await app.code())
  await page.getByRole('button', { name: form.confirm, exact: true }).click()
  return app
}

/** The recovery codes step: ten codes, confirmed as saved, then the button that leaves the step. */
async function saveRecoveryCodes(page: Page, continueLabel: string) {
  const codes = page.getByRole('list', { name: zh.recoveryCodes.title })
  await expect(codes.getByRole('listitem')).toHaveCount(10)
  await page.getByLabel(zh.recoveryCodes.confirm).check()
  await page.getByRole('button', { name: continueLabel, exact: true }).click()
}

/** The map shows the city by its configured name and has loaded at least one of its tiles. */
async function expectMap(page: Page, tile: Promise<unknown>) {
  await expect(page.locator('canvas.maplibregl-canvas')).toBeVisible()
  const select = page.getByLabel(zh.map.city, { exact: true })
  await expect(select).toHaveValue(city.slug)
  await expect(select.locator('option:checked')).toHaveText(city.name)
  await tile
  await expect(page.getByRole('banner')).toContainText(instanceName)
}

function tileLoaded(page: Page) {
  return page.waitForResponse((r) => tileRequest.test(r.url()) && r.status() === 200)
}

async function openMePage(page: Page) {
  await page.getByRole('navigation', { name: zh.nav.label }).getByRole('link', { name: zh.nav.me, exact: true }).click()
  await expect(page).toHaveURL(url('/me'))
}

async function signOut(page: Page) {
  await openMePage(page)
  await page.getByRole('button', { name: zh.pages.me.logout }).click()
  await expect(page).toHaveURL(url('/login'))
  expect((await page.context().cookies()).map((c) => c.name)).not.toContain('osfm_session')
}

test('the operator sets up the instance with the setup code from the server log', async () => {
  const { page } = maintainer
  await page.goto('/')
  await expect(page).toHaveURL(url('/setup'))

  await page.getByLabel(zh.setup.code.label, { exact: true }).fill(setupCode())
  await page.getByRole('button', { name: zh.setup.code.check, exact: true }).click()
  await page.getByLabel(zh.setup.name.label, { exact: true }).fill(instanceName)
  await page.getByRole('button', { name: zh.setup.name.next, exact: true }).click()

  // Password and TOTP is the method offered first.
  await registerWithPassword(page, 'test_maintainer', 'Test Maintainer')
  await saveRecoveryCodes(page, zh.setup.account.continue)

  // The generated archive is on the server already: the cities step offers it, to be named.
  const name = page.getByLabel(zh.setup.cities.name, { exact: true })
  await name.fill(city.name)
  const tile = tileLoaded(page)
  await page.getByRole('button', { name: zh.setup.cities.finish, exact: true }).click()
  await expect(page).toHaveURL(url('/'))
  await expectMap(page, tile)

  // The wizard is gone for good.
  await page.goto('/setup')
  await expect(page).toHaveURL(url('/'))
})

test('the maintainer creates two invites', async () => {
  const { page } = maintainer
  await openMePage(page)
  await page.getByRole('link', { name: zh.pages.me.admin }).click()
  await expect(page).toHaveURL(url('/admin'))
  const create = page.getByRole('button', { name: zh.pages.admin.invites.create })
  const link = page.getByLabel(zh.pages.admin.invites.link, { exact: true })
  for (let i = 0; i < 2; i++) {
    const previous = invites.at(-1) ?? ''
    await create.click()
    await expect(link).not.toHaveValue(previous)
    const value = await link.inputValue()
    expect(value).toMatch(new RegExp(`^${baseURL()}/register#invite=[A-Za-z0-9_-]{43}$`))
    invites.push(value)
  }
  await expect(page.getByRole('list', { name: zh.pages.admin.invites.title }).getByRole('listitem')).toHaveCount(2)
})

test('a member registers with password and TOTP', async () => {
  const { page } = alice
  await page.goto(invites[0] ?? '')
  await expect(page.getByText(zh.pages.register.validInvite)).toBeVisible()
  // The code is read from the link and removed from the address bar.
  await expect(page).toHaveURL(url('/register'))
  aliceApp = await registerWithPassword(page, 'test_alice', 'Test Alice')
  const tile = tileLoaded(page)
  await saveRecoveryCodes(page, zh.recoveryCodes.continue)
  await expect(page).toHaveURL(url('/'))
  await expectMap(page, tile)
})

test('a member registers with a passkey', async () => {
  const { page } = bob
  await page.goto(invites[1] ?? '')
  await expect(page.getByText(zh.pages.register.validInvite)).toBeVisible()
  await page
    .getByRole('group', { name: zh.pages.register.method })
    .getByRole('button', { name: zh.pages.register.methods.passkey, exact: true })
    .click()
  await page.getByLabel(zh.passkey.register.loginName, { exact: true }).fill('test_bob')
  await page.getByLabel(zh.passkey.register.displayName, { exact: true }).fill('Test Bob')
  await page.getByRole('button', { name: zh.passkey.register.submit, exact: true }).click()
  const tile = tileLoaded(page)
  await saveRecoveryCodes(page, zh.recoveryCodes.continue)
  await expect(page).toHaveURL(url('/'))
  await expectMap(page, tile)
})

test('the password member signs out and in again with a new TOTP code', async () => {
  const { page } = alice
  await signOut(page)
  const form = zh.pages.login.password
  await page.getByLabel(form.loginName, { exact: true }).fill('test_alice')
  await page.getByLabel(form.password, { exact: true }).fill(password)
  await page.getByRole('button', { name: form.next, exact: true }).click()
  const totp = page.getByLabel(form.totp, { exact: true })
  await expect(totp).toBeVisible()
  expect((await page.context().cookies()).map((c) => c.name)).not.toContain('osfm_session')
  // The code used at registration cannot be used again: this one is from a later time step.
  await totp.fill(await aliceApp.code())
  const tile = tileLoaded(page)
  await page.getByRole('button', { name: form.signIn, exact: true }).click()
  await expect(page).toHaveURL(url('/'))
  await expectMap(page, tile)
})

test('the passkey member signs out and in again with the passkey', async () => {
  const { page } = bob
  await signOut(page)
  await page
    .getByRole('group', { name: zh.pages.login.method })
    .getByRole('button', { name: zh.pages.login.methods.passkey, exact: true })
    .click()
  const tile = tileLoaded(page)
  await page.getByRole('button', { name: zh.passkey.signIn.button, exact: true }).click()
  await expect(page).toHaveURL(url('/'))
  await expectMap(page, tile)
})

test('a session ended on the server leads back to sign-in, and from there to the page asked for', async () => {
  const { page, context } = bob
  const cookie = (await context.cookies()).find((c) => c.name === 'osfm_session')
  expect(cookie).toBeDefined()
  // The server deletes the session while the browser still holds its cookie, as when a maintainer removes the member
  // or resets their credentials; here through the sign-out route, called outside the browser with a copy of the cookie.
  const elsewhere = await request.newContext({
    baseURL: baseURL(),
    extraHTTPHeaders: { Cookie: `osfm_session=${cookie?.value ?? ''}`, 'X-OSFM': '1' },
  })
  expect((await elsewhere.post('/api/osfm/logout')).status()).toBe(204)
  expect((await elsewhere.get('/api/osfm/me')).status()).toBe(401)
  await elsewhere.dispose()
  expect((await context.cookies()).map((c) => c.name)).toContain('osfm_session')

  await page.goto('/list')
  await expect(page).toHaveURL(url('/login?next=%2Flist'))
  await page
    .getByRole('group', { name: zh.pages.login.method })
    .getByRole('button', { name: zh.pages.login.methods.passkey, exact: true })
    .click()
  await page.getByRole('button', { name: zh.passkey.signIn.button, exact: true }).click()
  await expect(page).toHaveURL(url('/list'))
})
