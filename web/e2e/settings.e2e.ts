// The instance settings on the shared instance (/admin/settings): a maintainer changes the report threshold, the
// invite cooling-off period and place search in the browser, the server keeps them, and the name-and-cities form on the
// same page leaves them alone. Members learn only whether search is on. Everything is set back at the end for the other
// spec files. Made-up data only.

import { expect, type Page, test } from '@playwright/test'
import type { Instance, MaintainerInstance } from '../src/api/types.gen.ts'
import { attachScreenshots, close, type Person, takeProblems } from './support/browser.ts'
import { zh } from './support/env.ts'
import { apiAs, inviteMember, newSharedPerson, sharedInstance, sharedInstanceName } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

let maintainerPhone: Person
const people: Person[] = []

test.beforeAll(async ({ browser }) => {
  maintainerPhone = await newSharedPerson(browser, 'settings-maintainer', sharedInstance().maintainer)
  people.push(maintainerPhone)
})

// eslint-disable-next-line no-empty-pattern -- Playwright needs the fixtures argument destructured
test.afterEach(async ({}, testInfo) => {
  await attachScreenshots(people, testInfo)
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

// eslint-disable-next-line no-empty-pattern -- as above
test.afterAll(async ({}, testInfo) => {
  // The defaults again, for the spec files after this one.
  await apiAs(sharedInstance().maintainer).post('/instance', {
    reportThreshold: 3,
    inviteCooldownDays: 30,
    geocoderEnabled: true,
    geocoderContact: '',
  })
  await close(people, testInfo.project.outputDir)
})

const circle = zh.admin.circle

/** The circle settings section of the page and its controls. */
function circleForm(page: Page) {
  const section = page.getByRole('region', { name: circle.title })
  return {
    section,
    threshold: section.getByLabel(circle.reportThreshold, { exact: true }),
    cooldown: section.getByLabel(circle.inviteCooldownDays, { exact: true }),
    search: section.getByRole('checkbox', { name: circle.geocoderEnabled, exact: true }),
    contact: section.getByLabel(circle.geocoderContact, { exact: true }),
    save: section.getByRole('button', { name: circle.save, exact: true }),
  }
}

async function maintainerSettings(): Promise<MaintainerInstance> {
  return (await apiAs(sharedInstance().maintainer).get('/instance')) as MaintainerInstance
}

test('a maintainer changes the circle settings and they are kept', async () => {
  const { page } = maintainerPhone
  await page.goto('/admin/settings')
  const form = circleForm(page)
  // The saved values: the defaults of a new instance.
  await expect(form.threshold).toHaveValue('3')
  await expect(form.cooldown).toHaveValue('30')
  await expect(form.search).toBeChecked()
  await expect(form.contact).toHaveValue('')

  await form.threshold.fill('2')
  await form.cooldown.fill('7')
  await form.search.click()
  await form.contact.fill('test-contact@example.test')
  await form.save.click()
  await expect(form.section.getByText(circle.saved)).toBeVisible()

  await page.reload()
  await expect(form.threshold).toHaveValue('2')
  await expect(form.cooldown).toHaveValue('7')
  await expect(form.search).not.toBeChecked()
  await expect(form.contact).toHaveValue('test-contact@example.test')
  expect(await maintainerSettings()).toMatchObject({
    name: sharedInstanceName,
    reportThreshold: 2,
    inviteCooldownDays: 7,
    geocoderEnabled: false,
    geocoderContact: 'test-contact@example.test',
  })
})

test('the server refuses a threshold the form lets through only by mistake', async () => {
  // The form checks the threshold itself; the server checks it again.
  await expect(apiAs(sharedInstance().maintainer).post('/instance', { reportThreshold: 0 })).rejects.toMatchObject({
    status: 400,
    code: 'report_threshold_invalid',
  })
  expect((await maintainerSettings()).reportThreshold).toBe(2)
})

test('saving the name and cities keeps the circle settings', async () => {
  const { page } = maintainerPhone
  await page.goto('/admin/settings')
  const renamed = `${sharedInstanceName} Settings`
  for (const name of [renamed, sharedInstanceName]) {
    await page.getByLabel(zh.setup.name.label, { exact: true }).fill(name)
    await page.getByRole('button', { name: zh.admin.instance.save, exact: true }).click()
    await expect(page.getByText(zh.admin.instance.saved)).toBeVisible()
  }
  await page.reload()
  const form = circleForm(page)
  await expect(form.threshold).toHaveValue('2')
  await expect(form.cooldown).toHaveValue('7')
  await expect(form.search).not.toBeChecked()
  await expect(form.contact).toHaveValue('test-contact@example.test')
})

test('members learn only whether search is on', async () => {
  const member = await inviteMember()
  const seen = (await apiAs(member).get('/instance')) as Instance & Record<string, unknown>
  expect(seen.name).toBe(sharedInstanceName)
  expect(seen.geocoderEnabled).toBe(false)
  for (const field of ['reportThreshold', 'inviteCooldownDays', 'geocoderContact']) {
    expect(seen).not.toHaveProperty(field)
  }
  await expect(apiAs(member).post('/instance', { reportThreshold: 9 })).rejects.toMatchObject({
    status: 403,
    code: 'forbidden',
  })
})
