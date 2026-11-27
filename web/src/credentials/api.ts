// The member's own credentials, re-verification, recovery and reset links (D5; docs/architecture.md §3.3). Types are
// hand-written until they are generated from the server's structs (auth.CredentialsResponse and friends).

import { apiFetch, isJson } from '@/lib/api'
import { AuthApiError, field, isMe, postAuth, unexpected } from '@/lib/authApi'
import type { PasswordEnrolment } from '@/lib/passwordTotp'
import {
  assertionToJSON,
  creationOptionsFromJSON,
  PasskeyError,
  passkeyErrorFrom,
  postJson,
  readCeremonyStart,
  registrationToJSON,
  requestOptionsFromJSON,
} from '@/lib/webauthn'
import type { Me } from '@/session/session'

export interface PasskeyInfo {
  uuid: string
  deviceName: string
  createdAt: string
  /** The day (UTC midnight) of the last sign-in or re-verification with it. */
  lastUsedAt: string | null
}

export interface Credentials {
  passkeys: PasskeyInfo[]
  passwordTotp: { createdAt: string; lastUsedAt: string | null } | null
  recoveryCodesLeft: number
  /** Until when the current session counts as freshly verified; null when it does not. */
  verifiedUntil: string | null
}

export const credentialsQueryKey = ['credentials'] as const

/** The server's answer when a change needs a fresh proof first (HTTP 403). */
export const reauthRequiredCode = 'reauth_required'

function isNullableString(v: unknown): v is string | null {
  return v === null || typeof v === 'string'
}

function isPasskeyInfo(v: unknown): v is PasskeyInfo {
  return (
    typeof field(v, 'uuid') === 'string' &&
    typeof field(v, 'deviceName') === 'string' &&
    typeof field(v, 'createdAt') === 'string' &&
    isNullableString(field(v, 'lastUsedAt'))
  )
}

export function parseCredentials(body: unknown): Credentials {
  const passkeys = field(body, 'passkeys')
  const pw = field(body, 'passwordTotp')
  const left = field(body, 'recoveryCodesLeft')
  const until = field(body, 'verifiedUntil')
  if (
    !Array.isArray(passkeys) ||
    !passkeys.every(isPasskeyInfo) ||
    !(pw === null || (typeof field(pw, 'createdAt') === 'string' && isNullableString(field(pw, 'lastUsedAt')))) ||
    typeof left !== 'number' ||
    !isNullableString(until)
  ) {
    throw unexpected('/me/credentials')
  }
  return {
    passkeys,
    passwordTotp: pw === null ? null : (pw as Credentials['passwordTotp']),
    recoveryCodesLeft: left,
    verifiedUntil: until,
  }
}

/** GET /api/osfm/me/credentials. */
export async function fetchCredentials(): Promise<Credentials> {
  const response = await apiFetch('/me/credentials')
  const body: unknown = isJson(response) ? await response.json().catch(() => null) : null
  if (!response.ok) {
    const code = field(body, 'code')
    throw new AuthApiError(response.status, typeof code === 'string' ? code : null, '/me/credentials')
  }
  return parseCredentials(body)
}

/** Whether the session counts as freshly verified for at least another `marginMs`. */
export function isFresh(credentials: Credentials | undefined, now = Date.now(), marginMs = 60_000): boolean {
  if (!credentials?.verifiedUntil) {
    return false
  }
  return new Date(credentials.verifiedUntil).getTime() - now > marginMs
}

/** The error code of a failed request of either kind (AuthApiError or PasskeyError), or null. */
export function errorCode(error: unknown): string | null {
  if (error instanceof AuthApiError) {
    return error.code
  }
  if (error instanceof PasskeyError) {
    return error.code ?? null
  }
  return null
}

/** The seconds to wait after a rate_limited error of either kind, or null. */
export function errorRetryAfter(error: unknown): number | null {
  if (error instanceof AuthApiError || error instanceof PasskeyError) {
    return error.retryAfter
  }
  return null
}

/** POSTs to a "me" route and returns the credentials it answers with. */
async function postCredentials(path: string, body?: unknown): Promise<Credentials> {
  return parseCredentials(await postAuth(path, body ?? {}))
}

function verifiedUntil(path: string, body: unknown): string {
  const until = field(body, 'verifiedUntil')
  if (typeof until !== 'string') {
    throw unexpected(path)
  }
  return until
}

/** Re-verifies the session with the password and a TOTP code or a recovery code; returns until when it is fresh. */
export async function verifyWithPassword(password: string, code: string): Promise<string> {
  return verifiedUntil('/me/verify', await postAuth('/me/verify', { password, code }))
}

/** Re-verifies the session with one of the member's passkeys; returns until when it is fresh. */
export async function verifyWithPasskey(): Promise<string> {
  const start = await readCeremonyStart(await postJson('/me/verify/passkey/options'))
  let credential: Credential | null
  try {
    credential = await navigator.credentials.get(requestOptionsFromJSON(start.options))
  } catch (e) {
    throw passkeyErrorFrom(e)
  }
  if (!(credential instanceof PublicKeyCredential)) {
    throw new PasskeyError('failed')
  }
  const response = await postJson('/me/verify/passkey', { state: start.state, credential: assertionToJSON(credential) })
  return verifiedUntil('/me/verify/passkey', await response.json())
}

export function renamePasskey(uuid: string, deviceName: string): Promise<Credentials> {
  return postCredentials(`/me/passkeys/${encodeURIComponent(uuid)}/rename`, { deviceName })
}

export function removePasskey(uuid: string): Promise<Credentials> {
  return postCredentials(`/me/passkeys/${encodeURIComponent(uuid)}/remove`)
}

export function removePassword(): Promise<Credentials> {
  return postCredentials('/me/password-totp/remove')
}

export function changePassword(password: string): Promise<Credentials> {
  return postCredentials('/me/password', { password })
}

/** The new recovery codes, shown once; the old ones stop working. */
export async function regenerateRecoveryCodes(): Promise<string[]> {
  const codes = field(await postAuth('/me/recovery-codes', {}), 'recoveryCodes')
  if (!Array.isArray(codes) || !codes.every((c) => typeof c === 'string')) {
    throw unexpected('/me/recovery-codes')
  }
  return codes
}

/** Reads a pending enrolment (the start of a password and TOTP set-up). */
export function parseEnrolment(path: string, body: unknown): PasswordEnrolment {
  const enrolment = field(body, 'enrolment')
  const secret = field(body, 'secret')
  const otpauthUri = field(body, 'otpauthUri')
  const expiresAt = field(body, 'expiresAt')
  if (
    typeof enrolment !== 'string' ||
    typeof secret !== 'string' ||
    typeof otpauthUri !== 'string' ||
    !otpauthUri.startsWith('otpauth://totp/') ||
    typeof expiresAt !== 'string'
  ) {
    throw unexpected(path)
  }
  return { enrolment, secret, otpauthUri, expiresAt }
}

/**
 * Where a new credential is enrolled: on the "me" page (no extra body), after a recovery code ({ticket}) or with a
 * reset link ({token}). The endpoints are relative to /api/osfm.
 */
export interface EnrolTarget {
  passwordStart: string
  passwordConfirm: string
  passkeyOptions: string
  passkey: string
  body: Record<string, string>
}

export const meEnrolTarget: EnrolTarget = {
  passwordStart: '/me/password-totp/start',
  passwordConfirm: '/me/password-totp/confirm',
  passkeyOptions: '/me/passkeys/options',
  passkey: '/me/passkeys',
  body: {},
}

export function recoveryEnrolTarget(ticket: string): EnrolTarget {
  return {
    passwordStart: '/recover/password/start',
    passwordConfirm: '/recover/password/confirm',
    passkeyOptions: '/recover/passkey/options',
    passkey: '/recover/passkey',
    body: { ticket },
  }
}

export function resetEnrolTarget(token: string): EnrolTarget {
  return {
    passwordStart: '/reset/password/start',
    passwordConfirm: '/reset/password/confirm',
    passkeyOptions: '/reset/passkey/options',
    passkey: '/reset/passkey',
    body: { token },
  }
}

export async function startPasswordEnrolment(target: EnrolTarget, password: string): Promise<PasswordEnrolment> {
  return parseEnrolment(target.passwordStart, await postAuth(target.passwordStart, { ...target.body, password }))
}

/** Confirms a pending enrolment with one code; returns the server's response body. */
export function confirmPasswordEnrolment(target: EnrolTarget, enrolment: string, code: string): Promise<unknown> {
  return postAuth(target.passwordConfirm, { ...target.body, enrolment, code })
}

/** Creates a passkey for the target and returns the server's response body. */
export async function enrolPasskey(target: EnrolTarget, deviceName: string): Promise<unknown> {
  const start = await readCeremonyStart(await postJson(target.passkeyOptions, target.body))
  let credential: Credential | null
  try {
    credential = await navigator.credentials.create(creationOptionsFromJSON(start.options))
  } catch (e) {
    throw passkeyErrorFrom(e)
  }
  if (!(credential instanceof PublicKeyCredential)) {
    throw new PasskeyError('failed')
  }
  const response = await postJson(target.passkey, {
    ...target.body,
    state: start.state,
    credential: registrationToJSON(credential),
    deviceName,
  })
  return (await response.json()) as unknown
}

/** Starts replacing the TOTP secret. */
export async function startReplaceTotp(): Promise<PasswordEnrolment> {
  return parseEnrolment('/me/totp/start', await postAuth('/me/totp/start', {}))
}

export function confirmReplaceTotp(enrolment: string, code: string): Promise<Credentials> {
  return postCredentials('/me/totp/confirm', { enrolment, code })
}

/** What a recovery or reset enrolment ends with: the member, signed in, and the new recovery codes. */
export interface Enrolled {
  me: Me
  recoveryCodes: string[]
  otherPasskeys: number
}

export function parseEnrolled(body: unknown): Enrolled {
  const me = field(body, 'me')
  const codes = field(body, 'recoveryCodes')
  const other = field(body, 'otherPasskeys')
  if (!isMe(me) || !Array.isArray(codes) || !codes.every((c) => typeof c === 'string') || typeof other !== 'number') {
    throw new Error('enrolment: unexpected response')
  }
  return { me, recoveryCodes: codes, otherPasskeys: other }
}

/** Step 1 of the recovery of a passkey-only member: login name and a recovery code; returns the recovery ticket. */
export async function recover(loginName: string, code: string): Promise<string> {
  const ticket = field(await postAuth('/recover', { loginName, code }), 'ticket')
  if (typeof ticket !== 'string') {
    throw unexpected('/recover')
  }
  return ticket
}

/** Whether a reset token can be used, and whose account it resets. */
export async function checkResetToken(
  token: string,
): Promise<{ valid: false } | { valid: true; loginName: string; displayName: string }> {
  const body = await postAuth('/reset/check', { token })
  if (field(body, 'valid') !== true) {
    return { valid: false }
  }
  const loginName = field(body, 'loginName')
  const displayName = field(body, 'displayName')
  if (typeof loginName !== 'string' || typeof displayName !== 'string') {
    throw unexpected('/reset/check')
  }
  return { valid: true, loginName, displayName }
}

/** The token of a reset link from the URL fragment (#token=…), or null. */
export function resetTokenFromHash(hash: string): string | null {
  const token = new URLSearchParams(hash.replace(/^#/, '')).get('token')
  return token && /^[A-Za-z0-9_-]{43}$/.test(token) ? token : null
}
