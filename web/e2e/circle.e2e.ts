// A circle from its first start: the operator sets up the instance, the maintainer invites two members, one registers
// with password and TOTP and one with a passkey, both sign out and in again and look at the map, and a session ended
// on the server sends its member back to the sign-in page. Then the credential flows: the passkey member adds a second
// passkey and removes the first, regenerates the recovery codes, loses the phone and recovers with a recovery code on
// a new one; the maintainer, asked to re-verify first, creates a reset link from the member list, and the member resets
// to password and TOTP. Made-up data only.
//
// Every phone (browser context) is its own client of the authentication rate limits (docs/architecture.md §3.3; see
// support/browser.ts): each stays far below a burst of 30 requests and 20 failed attempts.

import { devices, expect, type Page, request, test } from '@playwright/test'
import { attachScreenshots, close, newPerson, PasskeyProvider, type Person, takeProblems } from './support/browser.ts'
import { baseURL, city, instanceName, setupCode, zh } from './support/env.ts'
import { makeSessionStale } from './support/session.ts'
import { AuthenticatorApp } from './support/totp.ts'

test.describe.configure({ mode: 'serial' })

const phone = devices['Pixel 7']
const password = 'test password, made up'
const tileRequest = new RegExp(`/tiles/${city.slug}/\\d+/\\d+/\\d+\\.mvt$`)

let maintainer: Person
let alice: Person // password and TOTP
let bob: Person // passkey
let people: Person[] = []
let maintainerApp: AuthenticatorApp
let aliceApp: AuthenticatorApp
let bobProvider: PasskeyProvider
let bobCodes: string[] = [] // the recovery codes Bob holds now
let oldBobCodes: string[] = [] // and those from before he regenerated them
let bobNew: Person // Bob's new phone after the recovery
const invites: string[] = []

test.beforeAll(async ({ browser }) => {
  maintainer = await newPerson(browser, 'maintainer', phone)
  alice = await newPerson(browser, 'alice', phone)
  bob = await newPerson(browser, 'bob', phone)
  people = [maintainer, alice, bob]
  // Bob's phone has a passkey provider for the whole test: its credentials live as long as the page.
  bobProvider = await PasskeyProvider.on(bob.page)
})

/** Another phone of one of the people, watched like the others. */
async function anotherPhone(browser: Parameters<typeof newPerson>[0], name: string): Promise<Person> {
  const person = await newPerson(browser, name, phone)
  people.push(person)
  return person
}

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

/** Reads the TOTP secret the page shows for typing into an app. */
async function authenticatorApp(page: Page): Promise<AuthenticatorApp> {
  await expect(page.getByRole('img', { name: zh.auth.register.password.qrTitle })).toBeVisible()
  return new AuthenticatorApp(await page.locator('code').innerText())
}

/** Fills the password form of registration (or setup) and confirms it with a code from the authenticator app. */
async function registerWithPassword(page: Page, loginName: string, displayName: string): Promise<AuthenticatorApp> {
  const form = zh.auth.register.password
  await page.getByLabel(form.loginName, { exact: true }).fill(loginName)
  await page.getByLabel(form.displayName, { exact: true }).fill(displayName)
  await page.getByLabel(form.password, { exact: true }).fill(password)
  await page.getByLabel(form.repeat, { exact: true }).fill(password)
  await page.getByRole('button', { name: form.next, exact: true }).click()
  const app = await authenticatorApp(page)
  // No session before the second factor.
  expect((await page.context().cookies()).map((c) => c.name)).not.toContain('osfm_session')
  await page.getByLabel(form.code, { exact: true }).fill(await app.code())
  await page.getByRole('button', { name: form.confirm, exact: true }).click()
  return app
}

/** The recovery codes step: ten codes, confirmed as saved, then the button that leaves the step. Returns the codes. */
async function saveRecoveryCodes(page: Page, continueLabel: string): Promise<string[]> {
  const list = page.getByRole('list', { name: zh.auth.recoveryCodes.title })
  await expect(list.getByRole('listitem')).toHaveCount(10)
  const codes = await list.getByRole('listitem').allInnerTexts()
  await page.getByLabel(zh.auth.recoveryCodes.confirm).check()
  await page.getByRole('button', { name: continueLabel, exact: true }).click()
  return codes
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
  await page
    .getByRole('navigation', { name: zh.common.nav.label })
    .getByRole('link', { name: zh.common.nav.me, exact: true })
    .click()
  await expect(page).toHaveURL(url('/me'))
}

/** Signs in with a passkey from the login page; the page must show it. */
async function signInWithPasskey(page: Page) {
  await page
    .getByRole('group', { name: zh.auth.login.method })
    .getByRole('button', { name: zh.auth.login.methods.passkey, exact: true })
    .click()
  await page.getByRole('button', { name: zh.auth.passkey.signIn.button, exact: true }).click()
}

async function signOut(page: Page) {
  await openMePage(page)
  await page.getByRole('button', { name: zh.me.logout }).click()
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
  maintainerApp = await registerWithPassword(page, 'test_maintainer', 'Test Maintainer')
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
  await page.getByRole('link', { name: zh.me.admin }).click()
  await expect(page).toHaveURL(url('/admin/invites'))
  const create = page.getByRole('button', { name: zh.invites.create })
  const link = page.getByLabel(zh.invites.link, { exact: true })
  for (let i = 0; i < 2; i++) {
    const previous = invites.at(-1) ?? ''
    await create.click()
    await expect(link).not.toHaveValue(previous)
    const value = await link.inputValue()
    expect(value).toMatch(new RegExp(`^${baseURL()}/register#invite=[A-Za-z0-9_-]{43}$`))
    invites.push(value)
  }
  await expect(page.getByRole('list', { name: zh.invites.title }).getByRole('listitem')).toHaveCount(2)
})

test('a member registers with password and TOTP', async () => {
  const { page } = alice
  await page.goto(invites[0] ?? '')
  await expect(page.getByText(zh.auth.register.validInvite)).toBeVisible()
  // The code is read from the link and removed from the address bar.
  await expect(page).toHaveURL(url('/register'))
  aliceApp = await registerWithPassword(page, 'test_alice', 'Test Alice')
  const tile = tileLoaded(page)
  await saveRecoveryCodes(page, zh.auth.recoveryCodes.continue)
  await expect(page).toHaveURL(url('/'))
  await expectMap(page, tile)
})

test('a member registers with a passkey', async () => {
  const { page } = bob
  await page.goto(invites[1] ?? '')
  await expect(page.getByText(zh.auth.register.validInvite)).toBeVisible()
  await page
    .getByRole('group', { name: zh.auth.register.method })
    .getByRole('button', { name: zh.auth.register.methods.passkey, exact: true })
    .click()
  await page.getByLabel(zh.auth.passkey.register.loginName, { exact: true }).fill('test_bob')
  await page.getByLabel(zh.auth.passkey.register.displayName, { exact: true }).fill('Test Bob')
  await page.getByRole('button', { name: zh.auth.passkey.register.submit, exact: true }).click()
  const tile = tileLoaded(page)
  bobCodes = await saveRecoveryCodes(page, zh.auth.recoveryCodes.continue)
  await expect(page).toHaveURL(url('/'))
  await expectMap(page, tile)
})

test('the password member signs out and in again with a new TOTP code', async () => {
  const { page } = alice
  await signOut(page)
  const form = zh.auth.login.password
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
    .getByRole('group', { name: zh.auth.login.method })
    .getByRole('button', { name: zh.auth.login.methods.passkey, exact: true })
    .click()
  const tile = tileLoaded(page)
  await page.getByRole('button', { name: zh.auth.passkey.signIn.button, exact: true }).click()
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
    .getByRole('group', { name: zh.auth.login.method })
    .getByRole('button', { name: zh.auth.login.methods.passkey, exact: true })
    .click()
  await page.getByRole('button', { name: zh.auth.passkey.signIn.button, exact: true }).click()
  await expect(page).toHaveURL(url('/list'))
})

/** A text of the locale file with its {{placeholders}} filled in. */
function text(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => String(values[key] ?? ''))
}

function passkeyList(page: Page) {
  return page.getByRole('list', { name: zh.auth.credentials.passkeys.title })
}

/** Removes the passkey in the row with the text, confirming once more. */
async function removePasskey(page: Page, rowText: string) {
  const row = passkeyList(page).getByRole('listitem').filter({ hasText: rowText })
  await row.getByRole('button', { name: zh.auth.credentials.remove, exact: true }).click()
  await row.getByRole('button', { name: zh.auth.credentials.confirmRemove, exact: true }).click()
  await expect(page.getByText(zh.auth.credentials.removed)).toBeVisible()
}

/** The recovery entry of the login page: login name and recovery code. */
async function startRecovery(page: Page, loginName: string, code: string) {
  const form = zh.auth.login.password
  await page.getByLabel(form.loginName, { exact: true }).fill(loginName)
  await page.getByLabel(form.recovery, { exact: true }).fill(code)
  await page.getByRole('button', { name: form.next, exact: true }).click()
}

// Bob's session is fresh (he has just signed in), so the changes need no re-verification here.
test('the passkey member adds a second passkey and removes the first, which then cannot sign in', async () => {
  const { page } = bob
  await openMePage(page)
  const list = passkeyList(page)
  await expect(list.getByRole('listitem')).toHaveCount(1)
  // The only way to sign in cannot be removed.
  await expect(list.getByRole('button', { name: zh.auth.credentials.remove, exact: true })).toBeDisabled()
  await expect(page.getByText(zh.auth.credentials.lastCredentialHint)).toBeVisible()

  // A second passkey provider on the phone (the first holds a passkey of this account already and would refuse).
  const first = await bobProvider.replace()
  await page.getByRole('button', { name: zh.auth.credentials.passkeys.add, exact: true }).click()
  await page.getByLabel(zh.auth.credentials.passkeys.deviceName, { exact: true }).fill('Test Phone B')
  await page.getByRole('button', { name: zh.auth.passkey.register.submit, exact: true }).click()
  await expect(page.getByText(zh.auth.credentials.added)).toBeVisible()
  await expect(list.getByRole('listitem')).toHaveCount(2)

  await removePasskey(page, zh.auth.credentials.passkeys.unnamed)
  await expect(list.getByRole('listitem')).toHaveCount(1)
  await expect(list.getByRole('listitem')).toContainText('Test Phone B')

  // With only the removed passkey, the sign-in fails; with the new one, it works.
  await signOut(page)
  const second = await bobProvider.replace(first)
  await signInWithPasskey(page)
  await expect(page.getByText(zh.auth.passkey.signIn.errors.failed)).toBeVisible()
  await bobProvider.replace(second)
  await page.getByRole('button', { name: zh.auth.passkey.signIn.button, exact: true }).click()
  await expect(page).toHaveURL(url('/'))
})

test('the passkey member regenerates the recovery codes', async () => {
  const { page } = bob
  await openMePage(page)
  await expect(page.getByText(text(zh.auth.credentials.recovery.left_other, { count: 10 }))).toBeVisible()
  await page.getByRole('button', { name: zh.auth.credentials.recovery.regenerate, exact: true }).click()
  const codes = await saveRecoveryCodes(page, zh.auth.credentials.done)
  await expect(page.getByText(zh.auth.credentials.recovery.regenerated)).toBeVisible()
  expect(codes.filter((c) => bobCodes.includes(c))).toEqual([])
  // The old codes are refused at the recovery below.
  oldBobCodes = bobCodes
  bobCodes = codes
})

test('the passkey member loses the phone and recovers with a recovery code on a new one', async ({ browser }) => {
  bobNew = await anotherPhone(browser, 'bob-new-phone')
  await PasskeyProvider.on(bobNew.page)
  const { page } = bobNew
  await page.goto('/login')
  await page.getByRole('button', { name: zh.auth.credentials.recover.entry, exact: true }).click()

  // A code from before the regeneration is refused.
  await startRecovery(page, 'test_bob', oldBobCodes[0] ?? '')
  await expect(page.getByText(zh.auth.credentials.recover.errors.failed)).toBeVisible()

  await startRecovery(page, 'test_bob', bobCodes[0] ?? '')
  await expect(page.getByText(zh.auth.credentials.recover.enrol)).toBeVisible()
  // The recovery is no session yet: a new credential first.
  expect((await page.context().cookies()).map((c) => c.name)).not.toContain('osfm_session')
  await page
    .getByRole('group', { name: zh.auth.credentials.enrol.method })
    .getByRole('button', { name: zh.auth.register.methods.passkey, exact: true })
    .click()
  await page.getByRole('button', { name: zh.auth.passkey.register.submit, exact: true }).click()
  bobCodes = await saveRecoveryCodes(page, zh.auth.credentials.recover.toMe)

  // On the "me" page, with the old passkey listed for removal.
  await expect(page).toHaveURL(url('/me'))
  await expect(page.getByText(zh.auth.credentials.recover.checkPasskeys)).toBeVisible()
  await expect(passkeyList(page).getByRole('listitem')).toHaveCount(2)
  await removePasskey(page, 'Test Phone B')
  await expect(passkeyList(page).getByRole('listitem')).toHaveCount(1)

  // The old phone's session has ended.
  await bob.page.goto('/list')
  await expect(bob.page).toHaveURL(url('/login?next=%2Flist'))
})

test('the maintainer sees the members, without login names', async () => {
  const { page } = maintainer
  await openMePage(page)
  await page.getByRole('link', { name: zh.me.admin }).click()
  await page
    .getByRole('navigation', { name: zh.admin.nav.label })
    .getByRole('link', { name: zh.admin.nav.members, exact: true })
    .click()
  await expect(page).toHaveURL(url('/admin/members'))
  const t = zh.members
  const rows = page.getByRole('list', { name: t.title }).getByRole('listitem')
  await expect(rows).toHaveCount(3)
  // In the order they joined; the other rows mention the maintainer as their inviter.
  const me = rows.first()
  await expect(me).toContainText('Test Maintainer')
  await expect(me).toContainText(`${t.maintainer} ${t.you}`)
  await expect(me).toContainText(text(t.methods, { methods: t.passwordTotp }))
  await expect(me.getByRole('button')).toHaveCount(0)
  await expect(rows.filter({ hasText: 'Test Alice' })).toContainText(text(t.methods, { methods: t.passwordTotp }))
  const bobRow = rows.filter({ hasText: 'Test Bob' })
  await expect(bobRow).toContainText(text(t.methods, { methods: text(t.passkeys_one, { count: 1 }) }))
  await expect(bobRow).toContainText(t.cannotInvite)
  expect(await page.locator('main').innerText()).not.toMatch(/test_(maintainer|alice|bob)/)
})

let resetLink = ''

test('the maintainer re-verifies when asked and creates a reset link from the member list', async () => {
  const { page, context } = maintainer
  // The maintainer signed in during the setup; ten minutes later the session is no longer fresh enough to create a
  // reset link. The page still counts it as fresh, so the server refuses and the page asks for a re-verification.
  await makeSessionStale(context)
  const bobRow = page
    .getByRole('list', { name: zh.members.title })
    .getByRole('listitem')
    .filter({ hasText: 'Test Bob' })
  const create = bobRow.getByRole('button', { name: zh.members.reset.create, exact: true })
  await create.click()
  const panel = page.getByRole('region', { name: zh.auth.credentials.verify.title })
  await expect(panel).toBeVisible()
  await expect(panel).toContainText(zh.auth.credentials.verify.repeat)
  await panel.getByLabel(zh.auth.login.password.password, { exact: true }).fill(password)
  await panel.getByLabel(zh.auth.credentials.verify.code, { exact: true }).fill(await maintainerApp.code())
  await panel.getByRole('button', { name: zh.auth.credentials.verify.submit, exact: true }).click()
  await expect(panel).toBeHidden()

  await create.click()
  const link = page.getByRole('textbox', { name: zh.members.reset.link, exact: true })
  await expect(link).toHaveValue(new RegExp(`^${baseURL()}/reset#token=[A-Za-z0-9_-]{43}$`))
  resetLink = await link.inputValue()
  await expect(bobRow).toContainText(zh.members.pendingReset.split('{{')[0] ?? '')
  const links = page.getByRole('list', { name: zh.members.reset.title }).getByRole('listitem')
  await expect(links).toHaveCount(1)
  await expect(links).toContainText(zh.invites.state.active)
})

test('the member resets to password and TOTP with the link; the old credentials and the link stop working', async ({
  browser,
}) => {
  const reset = await anotherPhone(browser, 'bob-reset')
  const { page } = reset
  await page.goto(resetLink)
  await expect(
    page.getByText(text(zh.auth.credentials.reset.account, { displayName: 'Test Bob', loginName: 'test_bob' })),
  ).toBeVisible()
  await expect(page).toHaveURL(url('/reset'))
  await page.getByLabel(zh.auth.credentials.enrol.password, { exact: true }).fill(password)
  await page.getByLabel(zh.auth.register.password.repeat, { exact: true }).fill(password)
  await page.getByRole('button', { name: zh.auth.register.password.next, exact: true }).click()
  const app = await authenticatorApp(page)
  await page.getByLabel(zh.auth.register.password.code, { exact: true }).fill(await app.code())
  await page.getByRole('button', { name: zh.auth.register.password.confirm, exact: true }).click()
  const tile = tileLoaded(page)
  const codes = await saveRecoveryCodes(page, zh.auth.recoveryCodes.continue)
  await expect(page).toHaveURL(url('/'))
  await expectMap(page, tile)

  // The phone from the recovery: its session has ended, its passkey and recovery codes no longer work.
  const old = bobNew.page
  await old.goto('/list')
  await expect(old).toHaveURL(url('/login?next=%2Flist'))
  await signInWithPasskey(old)
  await expect(old.getByText(zh.auth.passkey.signIn.errors.failed)).toBeVisible()
  await old.getByRole('button', { name: zh.auth.credentials.recover.entry, exact: true }).click()
  await startRecovery(old, 'test_bob', bobCodes[1] ?? '')
  await expect(old.getByText(zh.auth.credentials.recover.errors.failed)).toBeVisible()
  expect(codes.filter((c) => bobCodes.includes(c))).toEqual([])

  // The link works once.
  const again = await anotherPhone(browser, 'reset-again')
  await again.page.goto(resetLink)
  await expect(again.page.getByText(zh.auth.credentials.reset.invalid)).toBeVisible()

  // The maintainer's lists show the reset.
  const admin = maintainer.page
  await admin.reload()
  const t = zh.members
  const bobRow = admin.getByRole('list', { name: t.title }).getByRole('listitem').filter({ hasText: 'Test Bob' })
  await expect(bobRow).toContainText(text(t.methods, { methods: t.passwordTotp }))
  await expect(bobRow).not.toContainText(t.pendingReset.split('{{')[0] ?? '')
  await expect(admin.getByRole('list', { name: zh.members.reset.title }).getByRole('listitem')).toContainText(
    zh.invites.state.used,
  )
})
