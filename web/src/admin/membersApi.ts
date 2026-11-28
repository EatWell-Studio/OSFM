// The member list of the admin page (docs/architecture.md §3.3, §4.2): maintainers only. Members are named by uuid
// and main name; the login name is not part of it. Types are hand-written until they are generated from the server's
// structs (auth.MemberInfo). Maintainers switch a member's invite permission and open inviting early from it (D6, D12;
// the generated api.MemberInviteRights).

import type { InvitePermissionRequest, MemberInviteRights } from '@/api/types.gen'
import { apiJson } from '@/lib/api'
import { field, postAuth, unexpected } from '@/lib/authApi'
import type { MemberRef } from '@/invites/invites'

export interface MemberInfo {
  uuid: string
  displayName: string
  role: 'member' | 'maintainer'
  joinedAt: string
  canInvite: boolean
  /** When the member's invite cooling-off period ends or ended; null for maintainers, who may always invite. */
  inviteCooldownEndsAt: string | null
  /** Whether that period runs now. */
  inviteCooldown: boolean
  invitedBy: MemberRef | null
  /** When the member left or was removed; null while they are in the circle. */
  leftAt: string | null
  passkeys: number
  passwordTotp: boolean
  /** The member's reset link that can still be used. */
  pendingResetLink: { uuid: string; expiresAt: string } | null
}

export const membersQueryKey = ['members'] as const

function isRef(v: unknown): v is MemberRef | null {
  return v === null || (typeof field(v, 'uuid') === 'string' && typeof field(v, 'displayName') === 'string')
}

function isPending(v: unknown): v is MemberInfo['pendingResetLink'] {
  return v === null || (typeof field(v, 'uuid') === 'string' && typeof field(v, 'expiresAt') === 'string')
}

export function isMemberInfo(v: unknown): v is MemberInfo {
  const role = field(v, 'role')
  const leftAt = field(v, 'leftAt')
  const cooldown = field(v, 'inviteCooldownEndsAt')
  return (
    typeof field(v, 'uuid') === 'string' &&
    typeof field(v, 'displayName') === 'string' &&
    (role === 'member' || role === 'maintainer') &&
    typeof field(v, 'joinedAt') === 'string' &&
    typeof field(v, 'canInvite') === 'boolean' &&
    (cooldown === null || typeof cooldown === 'string') &&
    typeof field(v, 'inviteCooldown') === 'boolean' &&
    isRef(field(v, 'invitedBy')) &&
    (leftAt === null || typeof leftAt === 'string') &&
    typeof field(v, 'passkeys') === 'number' &&
    typeof field(v, 'passwordTotp') === 'boolean' &&
    isPending(field(v, 'pendingResetLink'))
  )
}

/** GET /api/osfm/members: the members in the order they joined, those who left included. */
export async function fetchMembers(): Promise<MemberInfo[]> {
  const list = field(await apiJson('/members'), 'members')
  if (!Array.isArray(list) || !list.every(isMemberInfo)) {
    throw unexpected('/members')
  }
  return list
}

function isRights(v: unknown): v is MemberInviteRights {
  const end = field(v, 'cooldownEndsAt')
  return (
    typeof field(v, 'uuid') === 'string' &&
    typeof field(v, 'canInvite') === 'boolean' &&
    typeof field(v, 'allowed') === 'boolean' &&
    typeof field(v, 'reason') === 'string' &&
    (end === null || typeof end === 'string')
  )
}

/** POST /api/osfm/members/{uuid}/invite-permission: switches the member's invite permission on or off. */
export async function setInvitePermission(uuid: string, canInvite: boolean): Promise<MemberInviteRights> {
  const path = `/members/${encodeURIComponent(uuid)}/invite-permission`
  const request: InvitePermissionRequest = { canInvite }
  const body = await postAuth(path, request)
  if (!isRights(body)) {
    throw unexpected(path)
  }
  return body
}

/** POST /api/osfm/members/{uuid}/invite-early: ends the member's cooling-off period now. */
export async function openInviteEarly(uuid: string): Promise<MemberInviteRights> {
  const path = `/members/${encodeURIComponent(uuid)}/invite-early`
  const body = await postAuth(path, {})
  if (!isRights(body)) {
    throw unexpected(path)
  }
  return body
}
