// One browser context per person of the tests (a phone each), watched the whole time: any violation of the content
// security policy, any request to another origin and any uncaught page error is recorded and fails the test it happens
// in (docs/architecture.md §4.7).
//
// Each phone has a client address of its own, as phones on different networks behind a deployment's reverse proxy do:
// the server runs with OSFM_TRUSTED_PROXY_HEADERS=X-Forwarded-For (global-setup.ts) and each context sends
// X-Forwarded-For with an address from a documentation range. The authentication rate limits (docs/architecture.md
// §3.3) then count every phone separately, as in a real circle, instead of all of them as one client at 127.0.0.1.

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

let phones = 0

/** A client address of its own for every phone, from 198.51.100.0/24 (TEST-NET-2). */
function nextClientAddress(): string {
  phones++
  if (phones > 254) {
    throw new Error('e2e: out of client addresses')
  }
  return `198.51.100.${String(phones)}`
}

export async function newPerson(browser: Browser, name: string, options: Parameters<Browser['newContext']>[0]) {
  const origin = new URL(baseURL()).origin
  const context = await browser.newContext({
    ...options,
    baseURL: baseURL(),
    extraHTTPHeaders: { ...options?.extraHTTPHeaders, 'X-Forwarded-For': nextClientAddress() },
  })
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

type Credential = Awaited<ReturnType<typeof credentialsOf>>[number]

async function credentialsOf(cdp: CDPSession, authenticatorId: string) {
  return (await cdp.send('WebAuthn.getCredentials', { authenticatorId })).credentials
}

/**
 * The passkey provider of a phone: a virtual platform authenticator for the page (Chrome DevTools Protocol), internal
 * transport, discoverable credentials, user verification that succeeds. Its credentials live as long as the page.
 */
export class PasskeyProvider {
  private readonly cdp: CDPSession
  private authenticatorId: string

  private constructor(cdp: CDPSession, authenticatorId: string) {
    this.cdp = cdp
    this.authenticatorId = authenticatorId
  }

  static async on(page: Page): Promise<PasskeyProvider> {
    const cdp = await page.context().newCDPSession(page)
    await cdp.send('WebAuthn.enable')
    return new PasskeyProvider(cdp, await PasskeyProvider.add(cdp))
  }

  private static async add(cdp: CDPSession): Promise<string> {
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
    return authenticatorId
  }

  /**
   * Replaces the provider by another one that holds only the given passkeys (none: an empty one, like a second passkey
   * provider on the phone) and returns the passkeys the old one held.
   */
  async replace(passkeys: Credential[] = []): Promise<Credential[]> {
    const held = await credentialsOf(this.cdp, this.authenticatorId)
    await this.cdp.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId: this.authenticatorId })
    this.authenticatorId = await PasskeyProvider.add(this.cdp)
    for (const credential of passkeys) {
      await this.cdp.send('WebAuthn.addCredential', { authenticatorId: this.authenticatorId, credential })
    }
    return held
  }
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
