// The shared instance of the feature specs (web/e2e/<feature>.e2e.ts): set up by the global setup through the API, with
// a maintainer and the made-up city, so that a spec starts from a configured circle instead of the setup wizard. Each
// spec file adds the members and places it needs with the helpers below and does not rely on what other spec files
// left behind (they run one after another on the same instance, in file name order).
//
// Rate limits (docs/architecture.md §3.3): every helper call and every phone has a client address of its own
// (X-Forwarded-For, as in support/browser.ts), so specs do not use up each other's budget. Do not sign the shared
// maintainer out or end its session: the other spec files use the same session. A flow that signs out or ends sessions
// uses a member of its own (inviteMember).
//
//   const maintainer = sharedInstance().maintainer
//   const alice = await inviteMember()
//   const phone = await newSharedPerson(browser, 'alice', alice)   // signed in, at sharedInstance().baseURL
//   await createPlace(alice, { name: 'Test Noodles', lat, lon, recommendation: { attitude: 'good', text: '…' } })

import { readFileSync } from 'node:fs'
import type { Browser, BrowserContext } from '@playwright/test'
import { devices } from '@playwright/test'
import type { CreatePlaceRequest } from '../../src/api/types.gen.ts'
import { newPerson, type Person } from './browser.ts'
import { city, sharedPortVariable, sharedStateVariable } from './env.ts'
import { AuthenticatorApp } from './totp.ts'

/** The session cookie's name on a local (http) instance. */
export const sessionCookie = 'osfm_session'

/** A member made through the API, with everything a spec needs to act as them. Made-up values only. */
export interface TestMember {
  uuid: string
  loginName: string
  displayName: string
  password: string
  /** The TOTP secret in base32. */
  totpSecret: string
  /** The time step of the code used at the registration (the server takes each step once). */
  totpStep: number
  /** The value of the session cookie of the session the registration started. */
  session: string
  recoveryCodes: string[]
}

export interface SharedInstance {
  /** The address the browser opens (localhost: the passkey relying party of a local instance). */
  baseURL: string
  /** The instance's data directory (for support/session.ts). */
  dataDir: string
  name: string
  maintainer: TestMember
}

export const sharedInstanceName = 'Test Circle Shared'

/** The authenticator app of a member, past the time step used already. */
export function authenticatorOf(member: TestMember): AuthenticatorApp {
  return new AuthenticatorApp(member.totpSecret, member.totpStep)
}

/** An unsuccessful response of the API. */
export class ApiFailure extends Error {
  readonly status: number
  readonly code: string | null

  constructor(method: string, path: string, status: number, code: string | null) {
    super(`${method} ${path}: HTTP ${String(status)}${code ? ` (${code})` : ''}`)
    this.status = status
    this.code = code
  }
}

let clients = 0

/** A client address of its own for every API client, from 203.0.113.0/24 (TEST-NET-3; the phones use TEST-NET-2). */
function nextClientAddress(): string {
  clients++
  if (clients > 254) {
    throw new Error('e2e: out of API client addresses')
  }
  return `203.0.113.${String(clients)}`
}

/**
 * A client of an instance's API (/api/osfm), as a browser would be: the session cookie it is given or the server sets,
 * X-OSFM on writes, and its own client address.
 */
export class ApiClient {
  session: string
  private readonly base: string
  private readonly address: string

  constructor(port: string, options: { session?: string; address?: string } = {}) {
    // 127.0.0.1: the server listens there only; the cookie does not depend on the host name.
    this.base = `http://127.0.0.1:${port}/api/osfm`
    this.session = options.session ?? ''
    this.address = options.address ?? nextClientAddress()
  }

  get(path: string): Promise<unknown> {
    return this.request('GET', path)
  }

  post(path: string, body?: unknown): Promise<unknown> {
    return this.request('POST', path, body)
  }

  private async request(method: string, path: string, body?: unknown): Promise<unknown> {
    const headers: Record<string, string> = { Accept: 'application/json', 'X-Forwarded-For': this.address }
    if (method !== 'GET') {
      headers['X-OSFM'] = '1'
    }
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json'
    }
    if (this.session) {
      headers.Cookie = `${sessionCookie}=${this.session}`
    }
    const response = await fetch(this.base + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    for (const cookie of response.headers.getSetCookie()) {
      const match = new RegExp(`^${sessionCookie}=([^;]*)`).exec(cookie)
      if (match) {
        this.session = match[1] ?? ''
      }
    }
    const text = await response.text()
    const parsed: unknown = text ? JSON.parse(text) : null
    if (!response.ok) {
      const code = (parsed as { code?: unknown } | null)?.code
      throw new ApiFailure(method, path, response.status, typeof code === 'string' ? code : null)
    }
    return parsed
  }
}

function field<T>(body: unknown, name: string, check: (v: unknown) => v is T): T {
  const value = (body as Record<string, unknown> | null)?.[name]
  if (!check(value)) {
    throw new Error(`e2e: unexpected response, no ${name}`)
  }
  return value
}

const isString = (v: unknown): v is string => typeof v === 'string'
const isStrings = (v: unknown): v is string[] => Array.isArray(v) && v.every(isString)
const isObject = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null

/**
 * Registers with password and TOTP through the API, with the entitlement (an invite, or the setup code with the
 * instance name), and returns the member with the session the registration started.
 */
async function registerWithPassword(
  client: ApiClient,
  paths: { start: string; confirm: string },
  entitlement: Record<string, string>,
  who: { loginName: string; displayName: string; password: string },
): Promise<TestMember> {
  const started = await client.post(paths.start, { ...entitlement, ...who })
  const secret = field(started, 'secret', isString)
  const app = new AuthenticatorApp(secret)
  const confirmed = await client.post(paths.confirm, {
    ...entitlement,
    enrolment: field(started, 'enrolment', isString),
    code: await app.code(),
  })
  const me = field(confirmed, 'me', isObject)
  if (!client.session) {
    throw new Error('e2e: the registration set no session cookie')
  }
  return {
    uuid: field(me, 'uuid', isString),
    ...who,
    totpSecret: secret,
    totpStep: app.usedStep,
    session: client.session,
    recoveryCodes: field(confirmed, 'recoveryCodes', isStrings),
  }
}

/**
 * Sets up the shared instance through the API, as the setup wizard does: the setup code, the instance name and the
 * first maintainer (password and TOTP), then the made-up city. Run by the global setup; returns the state the specs
 * read with sharedInstance().
 */
export async function setUpSharedInstance(port: string, dataDir: string, setupCode: string): Promise<SharedInstance> {
  // The global setup's own client address (TEST-NET-1), apart from the specs' API clients and phones.
  const client = new ApiClient(port, { address: '192.0.2.1' })
  await client.post('/setup/check', { code: setupCode })
  const maintainer = await registerWithPassword(
    client,
    { start: '/setup/password/start', confirm: '/setup/password/confirm' },
    { setupCode, instanceName: sharedInstanceName },
    { loginName: 'test_shared_maintainer', displayName: 'Test Shared Maintainer', password: 'test password, made up' },
  )
  const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
  await client.post('/setup/complete', {
    name: sharedInstanceName,
    cities: [
      {
        slug: city.slug,
        name: city.name,
        bbox: city.archive.bbox,
        center: [(minLon + maxLon) / 2, (minLat + maxLat) / 2],
      },
    ],
  })
  return { baseURL: `http://localhost:${port}`, dataDir, name: sharedInstanceName, maintainer }
}

let shared: SharedInstance | undefined

/** The shared instance as the global setup left it. */
export function sharedInstance(): SharedInstance {
  if (!shared) {
    const file = process.env[sharedStateVariable]
    if (!file) {
      throw new Error(`${sharedStateVariable} is not set: the global setup did not run`)
    }
    shared = JSON.parse(readFileSync(file, 'utf8')) as SharedInstance
  }
  return shared
}

function sharedPort(): string {
  const port = process.env[sharedPortVariable]
  if (!port) {
    throw new Error(`${sharedPortVariable} is not set: run the tests through playwright.config.ts`)
  }
  return port
}

const memberClients = new Map<string, ApiClient>()

/**
 * The API client of the shared instance acting as the member (their registration's session), one per member, at an
 * address of its own.
 */
export function apiAs(member: TestMember): ApiClient {
  let client = memberClients.get(member.uuid)
  if (!client) {
    client = new ApiClient(sharedPort(), { session: member.session })
    memberClients.set(member.uuid, client)
  }
  return client
}

let members = 0

/**
 * Invites a new member to the shared instance and registers them with password and TOTP, all through the API: the
 * shared maintainer creates the invite, or who.inviter (who must be allowed to invite). Names are unique per call
 * unless given. Returns the member, signed in.
 */
export async function inviteMember(
  who: { loginName?: string; displayName?: string; password?: string; inviter?: TestMember } = {},
): Promise<TestMember> {
  members++
  const suffix = `${Date.now().toString(36)}${members.toString(36)}`
  const invited = await apiAs(who.inviter ?? sharedInstance().maintainer).post('/invites', { days: 1 })
  return registerWithPassword(
    new ApiClient(sharedPort()),
    { start: '/register/password/start', confirm: '/register/password/confirm' },
    { invite: field(invited, 'code', isString) },
    {
      loginName: who.loginName ?? `test_m_${suffix}`,
      displayName: who.displayName ?? `Test Member ${suffix}`,
      password: who.password ?? 'test password, made up',
    },
  )
}

/** Puts the member's session cookie into the browser context: its pages are signed in as the member. */
export async function signIn(context: BrowserContext, member: TestMember) {
  await context.addCookies([
    { name: sessionCookie, value: member.session, url: sharedInstance().baseURL, httpOnly: true, sameSite: 'Lax' },
  ])
}

/**
 * A phone (support/browser.ts: watched for CSP violations, requests to other origins and page errors) at the shared
 * instance, signed in as the member when one is given. Pixel 7 unless other device options are given.
 */
export async function newSharedPerson(
  browser: Browser,
  name: string,
  member?: TestMember,
  options: Parameters<Browser['newContext']>[0] = devices['Pixel 7'],
): Promise<Person> {
  const person = await newPerson(browser, name, { ...options, baseURL: sharedInstance().baseURL })
  if (member) {
    await signIn(person.context, member)
  }
  return person
}

/**
 * A made-up place with its first recommendation: the body of POST /api/osfm/places (CreatePlaceRequest). Only name, lat,
 * lon and the recommendation's attitude and text are required; lat and lon must be inside the shared instance's city
 * (support/env.ts: city).
 */
export type FakePlace = CreatePlaceRequest

/**
 * Creates a made-up place with its first recommendation on the shared instance, as the member, through the API
 * (POST /api/osfm/places). Returns the UUIDs of both; an ApiFailure carries the error code when the server refuses.
 */
export async function createPlace(
  member: TestMember,
  place: FakePlace,
): Promise<{ place: string; recommendation: string }> {
  const created = await apiAs(member).post('/places', place)
  return { place: field(created, 'place', isString), recommendation: field(created, 'recommendation', isString) }
}
