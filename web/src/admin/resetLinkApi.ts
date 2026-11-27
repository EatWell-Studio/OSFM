// Reset links (D5; docs/architecture.md §3.3): maintainers create one for a member who lost every credential, list and
// revoke them. Types are hand-written until they are generated from the server's structs (auth.ResetLinkInfo).

import { apiJson } from '@/lib/api'
import { field, postAuth, unexpected } from '@/lib/authApi'
import { resetPath } from '@/lib/next'
import type { MemberRef } from '@/invites/invites'

export type ResetLinkState = 'active' | 'used' | 'expired' | 'revoked'

export interface ResetLink {
  uuid: string
  state: ResetLinkState
  member: MemberRef | null
  createdBy: MemberRef | null
  createdAt: string
  expiresAt: string
  usedAt: string | null
  revokedAt: string | null
}

export const resetLinksQueryKey = ['resetLinks'] as const

const states = new Set<string>(['active', 'used', 'expired', 'revoked'])

function isRef(v: unknown): v is MemberRef | null {
  return v === null || (typeof field(v, 'uuid') === 'string' && typeof field(v, 'displayName') === 'string')
}

function isNullableString(v: unknown): v is string | null {
  return v === null || typeof v === 'string'
}

export function isResetLink(v: unknown): v is ResetLink {
  const state = field(v, 'state')
  return (
    typeof field(v, 'uuid') === 'string' &&
    typeof state === 'string' &&
    states.has(state) &&
    isRef(field(v, 'member')) &&
    isRef(field(v, 'createdBy')) &&
    typeof field(v, 'createdAt') === 'string' &&
    typeof field(v, 'expiresAt') === 'string' &&
    isNullableString(field(v, 'usedAt')) &&
    isNullableString(field(v, 'revokedAt'))
  )
}

/** GET /api/osfm/reset-links, newest first. */
export async function fetchResetLinks(): Promise<ResetLink[]> {
  const list = field(await apiJson('/reset-links'), 'resetLinks')
  if (!Array.isArray(list) || !list.every(isResetLink)) {
    throw unexpected('/reset-links')
  }
  return list
}

/** Creates a reset link for the member with the login name; the token is shown this once. */
export async function createResetLink(loginName: string): Promise<{ resetLink: ResetLink; token: string }> {
  const body = await postAuth('/reset-links', { loginName })
  const resetLink = field(body, 'resetLink')
  const token = field(body, 'token')
  if (!isResetLink(resetLink) || typeof token !== 'string') {
    throw unexpected('/reset-links')
  }
  return { resetLink, token }
}

export async function revokeResetLink(uuid: string): Promise<ResetLink> {
  const path = `/reset-links/${encodeURIComponent(uuid)}/revoke`
  const resetLink = field(await postAuth(path, {}), 'resetLink')
  if (!isResetLink(resetLink)) {
    throw unexpected(path)
  }
  return resetLink
}

/** The link the member opens: the token in the fragment, so that it never reaches a server log. */
export function resetLinkUrl(origin: string, token: string): string {
  return `${origin}${resetPath}#token=${token}`
}
