// Backups on the shared instance (/admin/backup, G5): the maintainer, asked to re-verify first, writes a backup,
// downloads it in the browser (a zip with the manifest and the keys) and deletes it. Restoring is a command on the
// server and is tested by the Go integration tests (cmd/osfm/backup_test.go), as are the permissions of the four
// identities. Made-up data only.

import { readFile } from 'node:fs/promises'
import { expect, type Page, test } from '@playwright/test'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { zh } from './support/env.ts'
import { makeSessionStale } from './support/session.ts'
import { newSharedPerson, sharedInstance } from './support/shared.ts'
import { AuthenticatorApp } from './support/totp.ts'

test.describe.configure({ mode: 'serial' })

const t = zh.backup
let maintainerPhone: Person
const people: Person[] = []

test.beforeAll(async ({ browser }) => {
  maintainerPhone = await newSharedPerson(browser, 'backup-maintainer', sharedInstance().maintainer)
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

test('the maintainer re-verifies, writes a backup, downloads and deletes it', async () => {
  test.setTimeout(90_000) // the verification may wait for the next TOTP time step
  const { page, context } = maintainerPhone
  // Ten minutes after the last verification: the page asks for one before writing a backup.
  await makeSessionStale(context, sharedInstance().dataDir)
  await page.goto('/admin/backup')
  const section = page.getByRole('region', { name: t.title })
  await expect(section.getByText(t.secret, { exact: true })).toBeVisible()

  await section.getByRole('button', { name: t.create, exact: true }).click()
  await verify(page)
  const status = section.getByRole('status')
  await expect(status).toContainText(/osfm_\d{14}\.zip/)
  const name = /osfm_\d{14}\.zip/.exec((await status.textContent()) ?? '')?.[0] ?? ''
  const item = section.getByRole('listitem', { name, exact: true })
  await expect(item).toContainText(t.kind.osfm)

  // Downloading: the verification is still fresh, the file is saved at once.
  const [file] = await Promise.all([
    page.waitForEvent('download'),
    item.getByRole('button', { name: t.download, exact: true }).click(),
  ])
  expect(file.suggestedFilename()).toBe(name)
  const zip = await readFile(await file.path())
  expect(zip.subarray(0, 2).toString('latin1')).toBe('PK')
  // The central directory names the entries in plain text.
  for (const entry of ['data.db', 'osfm/manifest.json', 'osfm/secrets/master.key', 'osfm/places.geojson']) {
    expect(zip.includes(entry)).toBe(true)
  }

  await item.getByRole('button', { name: t.delete, exact: true }).click()
  await item.getByRole('button', { name: t.deleteConfirm, exact: true }).click()
  await expect(status).toHaveText(t.deleted.replace('{{name}}', name))
  await expect(section.getByRole('listitem', { name, exact: true })).toHaveCount(0)
})
