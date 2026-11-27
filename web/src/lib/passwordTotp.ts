// Registration and sign-in with password and TOTP (D3, D4; docs/architecture.md §3.3). Types are hand-written until
// they are generated from the server's structs (auth.RegisterPasswordStartResponse and friends).

import { field, isMe, postAuth, unexpected } from '@/lib/authApi'
import { type Entitlement, registrationPaths } from '@/lib/entitlement'
import type { Me } from '@/session/session'

export interface PasswordEnrolment {
  /** Sealed by the server; sent back unchanged with the confirming code. */
  enrolment: string
  /** The TOTP secret in base32, for typing into the authenticator app. */
  secret: string
  /** otpauth:// URI of the secret, for the QR code and for opening the app on this device. */
  otpauthUri: string
  expiresAt: string
}

export interface Registered {
  me: Me
  recoveryCodes: string[]
}

export interface SignedIn {
  me: Me
  /** After a recovery code: how many are left. */
  recoveryCodesLeft?: number
}

const passwordPath = '/signin/password'
const totpPath = '/signin/totp'
const recoveryPath = '/signin/recovery'

/**
 * Step 1 of the registration: checks the invite (or setup code), login name and password; returns the TOTP secret.
 * Creates nothing.
 */
export async function startPasswordRegistration(
  entitlement: Entitlement,
  req: { loginName: string; displayName: string; password: string },
): Promise<PasswordEnrolment> {
  const startPath = registrationPaths(entitlement).passwordStart
  const body = await postAuth(startPath, { ...entitlement, ...req })
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
    throw unexpected(startPath)
  }
  return { enrolment, secret, otpauthUri, expiresAt }
}

/** Step 2: one code from the app creates the member, starts the session and returns the recovery codes. */
export async function confirmPasswordRegistration(
  entitlement: Entitlement,
  req: { enrolment: string; code: string },
): Promise<Registered> {
  const confirmPath = registrationPaths(entitlement).passwordConfirm
  const body = await postAuth(confirmPath, { ...entitlement, ...req })
  const me = field(body, 'me')
  const codes = field(body, 'recoveryCodes')
  if (!isMe(me) || !Array.isArray(codes) || !codes.every((c) => typeof c === 'string')) {
    throw unexpected(confirmPath)
  }
  return { me, recoveryCodes: codes }
}

/** Sign-in step 1: login name and password; returns the ticket for the second step (not a session). */
export async function signInWithPassword(loginName: string, password: string): Promise<string> {
  const ticket = field(await postAuth(passwordPath, { loginName, password }), 'ticket')
  if (typeof ticket !== 'string') {
    throw unexpected(passwordPath)
  }
  return ticket
}

/** Sign-in step 2 with a TOTP code or a recovery code: starts the session. */
export async function signInWithCode(kind: 'totp' | 'recovery', ticket: string, code: string): Promise<SignedIn> {
  const path = kind === 'totp' ? totpPath : recoveryPath
  const body = await postAuth(path, { ticket, code })
  const me = field(body, 'me')
  const left = field(body, 'recoveryCodesLeft')
  if (!isMe(me) || (left !== undefined && typeof left !== 'number')) {
    throw unexpected(path)
  }
  return left === undefined ? { me } : { me, recoveryCodesLeft: left }
}

/** The login name as the server stores it: trimmed and lowercase. */
export function normalizeLoginName(s: string): string {
  return s.trim().toLowerCase()
}

/** Whether a login name (normalised) has the allowed form: 3 to 32 of a–z, 0–9, "_" and "-". */
export function isValidLoginName(s: string): boolean {
  return /^[a-z0-9_-]{3,32}$/.test(s)
}

export const minPasswordLength = 8
export const maxPasswordLength = 128

/** The password's length as the server counts it: code points (not grapheme clusters) after NFKC normalisation. */
export function passwordLength(password: string): number {
  return Array.from(password.normalize('NFKC')).length
}

/** A TOTP code as typed, without spaces; null unless it is 6 digits. */
export function normalizeTotpCode(s: string): string | null {
  const code = s.replace(/\s/g, '')
  return /^\d{6}$/.test(code) ? code : null
}

/** The base32 secret in groups of four, easier to type. */
export function groupSecret(secret: string): string {
  return secret.replace(/(.{4})(?=.)/g, '$1 ')
}
