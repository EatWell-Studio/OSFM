// Invites (D1, D2, D12; docs/architecture.md §3.3). Maintainers create, list and revoke them, and so do members once
// their cooling-off period is over (their own invites only); the registration page checks a code before showing the
// form. The invite types are hand-written until they are generated from the server's structs (auth.InviteInfo and
// friends); the invite status is generated (api.InviteStatus).

import type { InviteStatus } from '@/api/types.gen'
import { apiJson } from '@/lib/api'
import { postAuth } from '@/lib/authApi'
import { registerPath } from '@/lib/next'

export type InviteState = 'active' | 'used' | 'expired' | 'revoked'

export interface MemberRef {
  uuid: string
  displayName: string
}

export interface Invite {
  uuid: string
  state: InviteState
  createdAt: string
  expiresAt: string
  createdBy: MemberRef | null
  usedBy: MemberRef | null
  usedAt: string | null
  revokedAt: string | null
}

/** The validity choices offered when creating an invite, in days; the server accepts 1 to 30. */
export const inviteDayChoices = [1, 3, 7, 14, 30] as const
export const defaultInviteDays = 7

/** The list of invites; also the prefix of the invite status, so that invalidating it refreshes both. */
export const invitesQueryKey = ['invites'] as const
export const inviteStatusQueryKey = ['invites', 'status'] as const

/** The error codes of creating an invite (auth.CodeInviteNotAllowed, auth.CodeInviteLimitReached). */
export const inviteNotAllowedCode = 'invite_not_allowed'
export const inviteLimitReachedCode = 'invite_limit_reached'

const states = new Set<string>(['active', 'used', 'expired', 'revoked'])

function isMemberRef(v: unknown): v is MemberRef {
  if (typeof v !== 'object' || v === null) {
    return false
  }
  const r = v as Record<string, unknown>
  return typeof r.uuid === 'string' && typeof r.displayName === 'string'
}

function isNullableString(v: unknown): v is string | null {
  return v === null || typeof v === 'string'
}

export function isInvite(v: unknown): v is Invite {
  if (typeof v !== 'object' || v === null) {
    return false
  }
  const r = v as Record<string, unknown>
  return (
    typeof r.uuid === 'string' &&
    typeof r.state === 'string' &&
    states.has(r.state) &&
    typeof r.createdAt === 'string' &&
    typeof r.expiresAt === 'string' &&
    (r.createdBy === null || isMemberRef(r.createdBy)) &&
    (r.usedBy === null || isMemberRef(r.usedBy)) &&
    isNullableString(r.usedAt) &&
    isNullableString(r.revokedAt)
  )
}

function field(body: unknown, name: string): unknown {
  return typeof body === 'object' && body !== null ? (body as Record<string, unknown>)[name] : undefined
}

/** GET /api/osfm/invites: newest first; every invite for a maintainer, the member's own for a member. */
export async function fetchInvites(): Promise<Invite[]> {
  const list = field(await apiJson('/invites'), 'invites')
  if (!Array.isArray(list) || !list.every(isInvite)) {
    throw new Error('/invites: unexpected response')
  }
  return list
}

/**
 * POST /api/osfm/invites: a new invite and its code, which the server never shows again. A member who may not invite
 * gets an AuthApiError with inviteNotAllowedCode, one with too many active invites inviteLimitReachedCode.
 */
export async function createInvite(days: number): Promise<{ invite: Invite; code: string }> {
  const body = await postAuth('/invites', { days })
  const invite = field(body, 'invite')
  const code = field(body, 'code')
  if (!isInvite(invite) || typeof code !== 'string') {
    throw new Error('/invites: unexpected response')
  }
  return { invite, code }
}

/** POST /api/osfm/invites/{uuid}/revoke. */
export async function revokeInvite(uuid: string): Promise<Invite> {
  const path = `/invites/${encodeURIComponent(uuid)}/revoke`
  const invite = field(await apiJson(path, { method: 'POST' }), 'invite')
  if (!isInvite(invite)) {
    throw new Error(`${path}: unexpected response`)
  }
  return invite
}

function isNullableTime(v: unknown): v is string | null {
  return v === null || (typeof v === 'string' && !Number.isNaN(Date.parse(v)))
}

/** GET /api/osfm/me/invite-status: whether the signed-in member may invite now, and why not. */
export async function fetchInviteStatus(): Promise<InviteStatus> {
  const body = await apiJson('/me/invite-status')
  const allowed = field(body, 'allowed')
  const reason = field(body, 'reason')
  const cooldownEndsAt = field(body, 'cooldownEndsAt')
  const activeInvites = field(body, 'activeInvites')
  const maxActiveInvites = field(body, 'maxActiveInvites')
  if (
    typeof allowed !== 'boolean' ||
    typeof reason !== 'string' ||
    !isNullableTime(cooldownEndsAt) ||
    typeof activeInvites !== 'number' ||
    (maxActiveInvites !== null && typeof maxActiveInvites !== 'number')
  ) {
    throw new Error('/me/invite-status: unexpected response')
  }
  return { allowed, reason, cooldownEndsAt, activeInvites, maxActiveInvites }
}

const dayMs = 24 * 60 * 60 * 1000

/** The whole days left until end, counting a started day as a day; 0 once end has passed. */
export function daysLeft(end: string, now: number = Date.now()): number {
  return Math.max(0, Math.ceil((Date.parse(end) - now) / dayMs))
}

/**
 * POST /api/osfm/invites/check: whether the code can be used now. The server answers the same for every invalid code
 * (unknown, expired, used, revoked), so there is nothing more to tell the visitor than "invalid or expired".
 */
export async function checkInvite(code: string): Promise<boolean> {
  const valid = field(
    await apiJson('/invites/check', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code }),
    }),
    'valid',
  )
  if (typeof valid !== 'boolean') {
    throw new Error('/invites/check: unexpected response')
  }
  return valid
}

// The invite link is /register#invite=<code>. The code is in the fragment, which browsers never send: it stays out of
// the server's and any proxy's request logs and out of the Referer header. The registration page reads it and sends
// it to the server in a POST body.
const fragmentKey = 'invite'
const codePattern = /^[A-Za-z0-9_-]{43}$/

/** The invite link for a code, on the given origin (the instance's address). */
export function inviteLink(origin: string, code: string): string {
  return `${origin}${registerPath}#${new URLSearchParams({ [fragmentKey]: code }).toString()}`
}

/** The invite code in a location's fragment ("#invite=…"), as it is, or null when the fragment has none. */
export function inviteCodeFromHash(hash: string): string | null {
  return new URLSearchParams(hash.replace(/^#/, '')).get(fragmentKey)
}

/** Whether a string has the form of an invite code: 256 bits, base64url without padding. */
export function isWellFormedInviteCode(code: string): boolean {
  return codePattern.test(code)
}
