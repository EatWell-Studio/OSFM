// The service worker's caches (T043; docs/architecture.md §4.5), on an instance of its own with 1000 made-up places
// (support/perf.ts):
//
//   - N2: with the service worker active, reopening the app under Chrome DevTools' "Fast 4G" throttling shows the first
//     place marker within 3 s. The throttling (CDP Network.emulateNetworkConditions) is applied to the page and to the
//     service worker, whose own requests to the server it would otherwise not slow down. The numbers are attached to
//     the test (pwa-reopen-times.json);
//   - when the server does not answer (20 s of latency on the service worker's requests), the cached map data is used
//     after the timeout of 3 s;
//   - no-store answers (the place card, the profile, the instance settings) are never cached;
//   - signing out leaves no /api/osfm/ or /tiles/ entry in any cache, and neither does the server ending the session;
//   - another member on the same device starts with the private caches emptied.
//
// Made-up data only; nothing is asked of another origin.

import { createHash } from 'node:crypto'
import { writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { DatabaseSync } from 'node:sqlite'
import { type Browser, devices, expect, type Page, test } from '@playwright/test'
import { attachScreenshots, close, newPerson, type Person, takeProblems } from './support/browser.ts'
import { zh } from './support/env.ts'
import { type PerfInstance, startPerfInstance } from './support/perf.ts'
import { sessionCookie, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

/**
 * Chrome DevTools' "Fast 4G" preset (devtools-frontend, NetworkManager.ts): 9 Mbit/s down and 1.5 Mbit/s up at 90 %,
 * 60 ms × 2.75 of latency added to every request.
 */
const fast4G = {
  offline: false,
  latency: 60 * 2.75,
  downloadThroughput: ((9 * 1000 * 1000) / 8) * 0.9,
  uploadThroughput: ((1.5 * 1000 * 1000) / 8) * 0.9,
}

/** N2: from opening the app to the first place marker on the map. */
const firstMarkerBudgetMs = 3000
const reopenRuns = 3

const placeLayers = ['osfm-place-clusters', 'osfm-place-circles', 'osfm-place-pins']

interface MapLike {
  getLayer(id: string): unknown
  queryRenderedFeatures(options: { layers: string[] }): unknown[]
  loaded(): boolean
}
type HookedWindow = Window & { osfmTestHooks?: { map?: MapLike }; osfmFirstMarker?: number }

let perf: PerfInstance
const people: Person[] = []

// eslint-disable-next-line no-empty-pattern -- Playwright needs the fixtures argument destructured
test.beforeAll(async ({}, testInfo) => {
  test.setTimeout(300_000)
  perf = await startPerfInstance(testInfo, 'osfm-server-pwa.log')
})

test.afterAll(async () => {
  await perf.stop()
})

// eslint-disable-next-line no-empty-pattern -- Playwright needs the fixtures argument destructured
test.afterEach(async ({}, testInfo) => {
  await attachScreenshots(people, testInfo)
  await close(people.splice(0), testInfo.project.outputDir)
})

/**
 * A phone at the instance, signed in with the session cookie, whose pages record when the first place marker was
 * drawn: window.osfmFirstMarker, in ms since the navigation started (performance.now()).
 */
async function phone(browser: Browser, name: string, session: string): Promise<Person> {
  const person = await newPerson(browser, name, { ...devices['Pixel 7'], baseURL: perf.baseURL })
  people.push(person)
  await person.context.addCookies([
    { name: sessionCookie, value: session, url: perf.baseURL, httpOnly: true, sameSite: 'Lax' },
  ])
  await person.context.addInitScript((layers) => {
    const w = window as HookedWindow
    w.osfmTestHooks = {}
    const tick = () => {
      const map = w.osfmTestHooks?.map
      const present = map ? layers.filter((id) => map.getLayer(id)) : []
      if (map && present.length > 0 && map.queryRenderedFeatures({ layers: present }).length > 0) {
        w.osfmFirstMarker = performance.now()
        return
      }
      requestAnimationFrame(tick)
    }
    requestAnimationFrame(tick)
  }, placeLayers)
  return person
}

async function firstMarker(page: Page): Promise<number> {
  const handle = await page.waitForFunction(() => (window as HookedWindow).osfmFirstMarker, undefined, {
    timeout: 30_000,
  })
  return Math.round(Number(await handle.jsonValue()))
}

/** Waits until the service worker controls the page (it takes over the open pages when it first activates). */
async function serviceWorkerInControl(page: Page) {
  await page.waitForFunction(() => navigator.serviceWorker.controller !== null, undefined, { timeout: 30_000 })
}

/** Every entry of every cache of the origin, as seen from the page. */
function cachedURLs(page: Page): Promise<{ cache: string; url: string }[]> {
  return page.evaluate(async () => {
    const entries: { cache: string; url: string }[] = []
    for (const name of await caches.keys()) {
      const cache = await caches.open(name)
      for (const request of await cache.keys()) {
        entries.push({ cache: name, url: request.url })
      }
    }
    return entries
  })
}

/** The entries that hold a member's data: the API's and the tiles'. */
async function privateEntries(page: Page): Promise<string[]> {
  return (await cachedURLs(page))
    .map((e) => new URL(e.url).pathname + new URL(e.url).search)
    .filter((path) => path.startsWith('/api/osfm/') || path.startsWith('/tiles/'))
}

type Conditions = typeof fast4G

/** Network conditions for the page (DevTools' network throttling). */
async function throttlePage(page: Page, conditions: Conditions) {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Network.enable')
  await cdp.send('Network.emulateNetworkConditions', conditions)
}

/**
 * Network conditions for the service worker. It is a target of its own: the page's conditions do not slow down its
 * requests to the server. Playwright's CDP sessions are for pages only, so the browser's session relays the commands.
 * Returns a function that sets other conditions on the same session.
 */
async function throttleServiceWorker(browser: Browser, conditions: Conditions) {
  const browserCDP = await browser.newBrowserCDPSession()
  const { targetInfos } = await browserCDP.send('Target.getTargets')
  const workers = targetInfos.filter((t) => t.type === 'service_worker' && t.url.startsWith(perf.baseURL))
  expect(workers, 'the service worker is running').toHaveLength(1)
  const worker = workers[0]
  if (!worker) {
    throw new Error('e2e: no service worker')
  }
  let id = 0
  const answers = new Map<number, (message: { error?: unknown }) => void>()
  browserCDP.on('Target.receivedMessageFromTarget', (event) => {
    const message = JSON.parse(event.message) as { id?: number; error?: unknown }
    if (message.id !== undefined) {
      answers.get(message.id)?.(message)
    }
  })
  const { sessionId } = await browserCDP.send('Target.attachToTarget', { targetId: worker.targetId, flatten: false })
  const send = async (method: string, params: object) => {
    id++
    const answer = new Promise<{ error?: unknown }>((done) => answers.set(id, done))
    await browserCDP.send('Target.sendMessageToTarget', { sessionId, message: JSON.stringify({ id, method, params }) })
    expect((await answer).error, method).toBeUndefined()
  }
  await send('Network.enable', {})
  await send('Network.emulateNetworkConditions', conditions)
  return (other: Conditions) => send('Network.emulateNetworkConditions', other)
}

/** Throttles the page and the service worker as DevTools' "Fast 4G" does. */
async function throttle(browser: Browser, page: Page) {
  await throttlePage(page, fast4G)
  await throttleServiceWorker(browser, fast4G)
}

interface Timing {
  firstMarkerMs: number
  /** The requests of the start, by path: when each ended (ms since the navigation started) and how long it took. */
  requests: Record<string, { endMs: number; durationMs: number }>
}

async function timing(page: Page, firstMarkerMs: number): Promise<Timing> {
  const requests = await page.evaluate(() =>
    Object.fromEntries(
      (performance.getEntriesByType('resource') as PerformanceResourceTiming[])
        .filter((e) => /\/api\/osfm\/(me|instance|places\?)|\/tiles\/$/.test(e.name))
        .map((e) => [
          new URL(e.name).pathname + (new URL(e.name).search ? '?…' : ''),
          { endMs: Math.round(e.responseEnd), durationMs: Math.round(e.duration) },
        ]),
    ),
  )
  return { firstMarkerMs, requests }
}

test('reopening with the service worker active, under Fast 4G: first place marker within 3 s (N2)', async ({
  browser,
}, testInfo) => {
  test.setTimeout(180_000)
  const person = await phone(browser, 'pwa-reopen', perf.session)

  // The first visit installs the service worker, which takes over the page; a second load, with the service worker in
  // control, puts the map data and the tiles of the start into its caches. Both unthrottled: only reopening is measured.
  const { page: first } = person
  await first.goto('/')
  await firstMarker(first)
  await serviceWorkerInControl(first)
  await first.reload()
  await firstMarker(first)
  await expect.poll(async () => (await privateEntries(first)).some((p) => p.startsWith('/api/osfm/places?'))).toBe(true)
  await expect
    .poll(async () => (await privateEntries(first)).filter((p) => p.endsWith('.mvt')).length)
    .toBeGreaterThan(0)
  await first.close()

  // Reopening: a new page, throttled, the service worker active.
  const reopens: Timing[] = []
  for (let run = 0; run < reopenRuns; run++) {
    const page = await person.context.newPage()
    await throttle(browser, page)
    await page.goto('/')
    reopens.push(await timing(page, await firstMarker(page)))
    // The map data came from the network through the service worker, slowed down like the page: at least the latency.
    const places = Object.entries(reopens.at(-1)?.requests ?? {}).find(([path]) => path.startsWith('/api/osfm/places'))
    expect(places?.[1].durationMs ?? 0, 'the service worker is throttled too').toBeGreaterThanOrEqual(fast4G.latency)
    await page.close()
  }

  const result = {
    conditions: { name: 'Fast 4G (Chrome DevTools)', ...fast4G },
    places: 1000,
    browser: browser.version(),
    reopenWithServiceWorker: reopens,
    reopenFirstMarkerMs: reopens.map((r) => r.firstMarkerMs),
  }
  console.log(`pwa: first place marker under Fast 4G:\n${JSON.stringify(result, null, 2)}`)
  writeFileSync(testInfo.outputPath('pwa-reopen-times.json'), JSON.stringify(result, null, 2))
  await testInfo.attach('pwa-reopen-times.json', {
    body: JSON.stringify(result, null, 2),
    contentType: 'application/json',
  })
  expect(Math.max(...result.reopenFirstMarkerMs)).toBeLessThanOrEqual(firstMarkerBudgetMs)
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

test('a stalled network: the cached map data after the timeout', async ({ browser }) => {
  test.setTimeout(120_000)
  const person = await phone(browser, 'pwa-stalled', perf.session)
  const { page: first } = person
  await first.goto('/')
  await firstMarker(first)
  await serviceWorkerInControl(first)
  await first.reload()
  await firstMarker(first)
  await expect.poll(async () => (await privateEntries(first)).some((p) => p.startsWith('/api/osfm/places?'))).toBe(true)
  await first.close()

  // The page's own requests (the session) are fast; whatever the service worker asks the server takes 20 s.
  const stalled = { ...fast4G, latency: 20_000, downloadThroughput: -1, uploadThroughput: -1 }
  const setConditions = await throttleServiceWorker(browser, stalled)
  try {
    const page = await person.context.newPage()
    await page.goto('/')
    // The city index and then the places each wait for the timeout (3 s) before the cached copy is used.
    const ms = await firstMarker(page)
    console.log(`pwa: first place marker with a stalled network: ${String(ms)} ms`)
    expect(ms).toBeGreaterThan(2 * 3000)
    expect(ms).toBeLessThan(15_000)
  } finally {
    await setConditions({ offline: false, latency: 0, downloadThroughput: -1, uploadThroughput: -1 })
  }
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

test('no-store answers are never cached, and signing out empties the private caches', async ({ browser }) => {
  const person = await phone(browser, 'pwa-sign-out', perf.session)
  const { page } = person
  await page.goto('/')
  await firstMarker(page)
  await serviceWorkerInControl(page)
  await expect.poll(async () => (await privateEntries(page)).length).toBeGreaterThan(0)

  // The card of a place (no-store), then the member's page (the profile and more, all no-store).
  const uuid = await page.evaluate(async () => {
    const body = (await (await fetch('/api/osfm/places?city=example-bay')).json()) as { uuid: string }[]
    return body[0]?.uuid ?? ''
  })
  await page.goto(`/p/${uuid}`)
  await expect(page.getByRole('heading', { name: /^Test Perf Place / })).toBeVisible()
  await page.goto('/me')
  await expect(page.getByRole('button', { name: zh.me.logout })).toBeVisible()
  const cached = await privateEntries(page)
  expect(
    cached.filter(
      (p) => !p.startsWith('/api/osfm/places?') && !p.startsWith('/api/osfm/signatures?') && !p.startsWith('/tiles/'),
    ),
  ).toEqual([])
  expect(cached.some((p) => p.startsWith(`/api/osfm/places/${uuid}`))).toBe(false)
  expect(await page.evaluate(() => localStorage.getItem('osfm.cacheOwner'))).not.toBeNull()

  await page.getByRole('button', { name: zh.me.logout }).click()
  await expect(page).toHaveURL(/\/login$/)
  expect(await privateEntries(page)).toEqual([])
  // Only the private caches went: the app itself stays cached.
  expect((await cachedURLs(page)).some((e) => new URL(e.url).pathname === '/index.html')).toBe(true)
  expect(await page.evaluate(() => localStorage.getItem('osfm.cacheOwner'))).toBeNull()
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

/** Ends a session on the server, as removing the member or resetting their credentials does (its row goes). */
function endSession(session: string) {
  const hash = createHash('sha256').update(session).digest('hex')
  const db = new DatabaseSync(join(perf.dataDir, 'pb_data', 'data.db'))
  try {
    db.exec('PRAGMA busy_timeout = 5000')
    expect(db.prepare('DELETE FROM sessions WHERE token_hash = ?').run(hash).changes).toBe(1)
  } finally {
    db.close()
  }
}

function member(i: number): TestMember {
  const m = perf.members[i]
  if (!m) {
    throw new Error(`e2e: no perf member ${String(i)}`)
  }
  return m
}

test('a session the server ended empties the private caches', async ({ browser }) => {
  const alice = member(0)
  const person = await phone(browser, 'pwa-ended', alice.session)
  const { page } = person
  await page.goto('/')
  await firstMarker(page)
  await serviceWorkerInControl(page)
  await expect.poll(async () => (await privateEntries(page)).length).toBeGreaterThan(0)

  endSession(alice.session)
  await page.reload()
  await expect(page).toHaveURL(/\/login\?next=/)
  await expect.poll(() => privateEntries(page)).toEqual([])
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})

test('another member on the same device starts with the private caches emptied', async ({ browser }) => {
  const bob = member(1)
  const carol = member(2)
  const person = await phone(browser, 'pwa-shared-device', bob.session)
  const { page, context } = person
  await page.goto('/')
  await firstMarker(page)
  await serviceWorkerInControl(page)
  await expect.poll(async () => (await privateEntries(page)).length).toBeGreaterThan(0)

  // Carol's session on the device, without Bob signing out (his cookie simply replaced).
  await context.clearCookies()
  await context.addCookies([
    { name: sessionCookie, value: carol.session, url: perf.baseURL, httpOnly: true, sameSite: 'Lax' },
  ])
  // The member's page fetches no map data: whatever is cached now was left from before.
  await page.goto('/me')
  await expect(page.getByRole('button', { name: zh.me.logout })).toBeVisible()
  expect(await privateEntries(page)).toEqual([])
  expect(await page.evaluate(() => localStorage.getItem('osfm.cacheOwner'))).toBe(carol.uuid)

  // Her own map is cached as usual.
  await page.goto('/')
  await firstMarker(page)
  await expect.poll(async () => (await privateEntries(page)).length).toBeGreaterThan(0)
  expect(takeProblems(people), 'CSP violations, requests to other origins or page errors').toEqual([])
})
