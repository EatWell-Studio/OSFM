// One browser context per person of the tests (a phone each), watched the whole time: any violation of the content
// security policy, any request to another origin and any uncaught page error is recorded and fails the test it happens
// in (docs/architecture.md §4.7).

import type { Browser, BrowserContext, CDPSession, Page, TestInfo } from '@playwright/test'
import { baseURL } from './env.ts'

// Reported from the page by the init script below; Chromium also logs its own "Refused to …" messages.
const cspMarker = 'osfm-e2e: CSP violation'
const cspConsole = /Content Security Policy|content-security-policy/i

export interface Person {
  name: string
  context: BrowserContext
  page: Page
  /** Problems seen since the last call of takeProblems. */
  problems: string[]
}

export async function newPerson(browser: Browser, name: string, options: Parameters<Browser['newContext']>[0]) {
  const origin = new URL(baseURL()).origin
  const context = await browser.newContext({ ...options, baseURL: baseURL() })
  await context.tracing.start({ screenshots: true, snapshots: true, title: name })
  const person: Person = { name, context, page: await context.newPage(), problems: [] }
  await context.addInitScript((marker) => {
    document.addEventListener('securitypolicyviolation', (e) => {
      console.error(`${marker} ${e.violatedDirective} ${e.blockedURI} at ${e.sourceFile}:${String(e.lineNumber)}`)
    })
  }, cspMarker)
  context.on('request', (request) => {
    const url = new URL(request.url())
    if (url.origin !== origin && url.protocol !== 'data:' && url.protocol !== 'blob:') {
      person.problems.push(`request to another origin: ${request.method()} ${request.url()}`)
    }
  })
  const watch = (page: Page) => {
    page.on('console', (message) => {
      const text = message.text()
      if (text.includes(cspMarker) || cspConsole.test(text)) {
        person.problems.push(`${page.url()}: ${text}`)
      }
    })
    page.on('pageerror', (error) => {
      person.problems.push(`${page.url()}: uncaught ${error.name}: ${error.message}`)
    })
  }
  context.on('page', watch)
  watch(person.page)
  return person
}

/**
 * A virtual platform authenticator for the page (Chrome DevTools Protocol): internal transport, discoverable
 * credentials, user verification that succeeds, like a phone's passkey provider.
 */
export async function addVirtualAuthenticator(page: Page): Promise<{ cdp: CDPSession; authenticatorId: string }> {
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2',
      transport: 'internal',
      hasResidentKey: true,
      hasUserVerification: true,
      isUserVerified: true,
      automaticPresenceSimulation: true,
    },
  })
  return { cdp, authenticatorId }
}

/** Fails with the problems recorded since the last call. */
export function takeProblems(people: Person[]): string[] {
  return people.flatMap((p) => p.problems.splice(0).map((problem) => `${p.name}: ${problem}`))
}

/** On failure, attaches a screenshot of every person's page; always keeps each person's trace in the output. */
export async function attachScreenshots(people: Person[], testInfo: TestInfo) {
  if (testInfo.status === testInfo.expectedStatus) {
    return
  }
  for (const p of people) {
    const shot = await p.page.screenshot({ fullPage: true }).catch(() => null)
    if (shot) {
      await testInfo.attach(`${p.name}.png`, { body: shot, contentType: 'image/png' })
    }
  }
}

export async function close(people: Person[], outputDir: string) {
  for (const p of people) {
    await p.context.tracing.stop({ path: `${outputDir}/trace-${p.name}.zip` }).catch(() => undefined)
    await p.context.close()
  }
}
