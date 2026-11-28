// The member's own profile (D8, C3, M2-Q6): GET and POST /api/osfm/me/profile (internal/api/profile.go). Other features
// read it through profileQuery: the recommendation form offers signing with the pseudonym only when hasPseudonym.

import { queryOptions } from '@tanstack/react-query'
import {
  type ExternalLink,
  MaxExternalLinkLabelLength,
  MaxExternalLinks,
  MaxExternalLinkURLLength,
  type Profile,
  ProfilePath,
  type ProfileUpdate,
} from '@/api/types.gen'
import { apiFetch, apiJson, isJson, retryAfterSeconds } from '@/lib/api'
import { field } from '@/lib/authApi'

export type { ExternalLink, Profile, ProfileUpdate }

/** The path of the profile relative to the API base (apiFetch takes paths under /api/osfm). */
const path = ProfilePath.replace(/^\/api\/osfm/, '')

export const profileQueryKey = ['me', 'profile'] as const

function isLink(value: unknown): value is ExternalLink {
  const label = field(value, 'label')
  return typeof field(value, 'url') === 'string' && (label === undefined || typeof label === 'string')
}

/** Reads a profile from a response body; throws when it does not have the shape of one. */
export function parseProfile(body: unknown): Profile {
  const displayName = field(body, 'displayName')
  const pseudonym = field(body, 'pseudonym')
  const links = field(body, 'externalLinks')
  if (
    typeof displayName !== 'string' ||
    typeof pseudonym !== 'string' ||
    !Array.isArray(links) ||
    !links.every(isLink)
  ) {
    throw new Error(`${ProfilePath}: unexpected response`)
  }
  return {
    displayName,
    pseudonym,
    externalLinks: links.map((l) => (l.label ? { url: l.url, label: l.label } : { url: l.url })),
    pseudonymInUse: field(body, 'pseudonymInUse') === true,
    displayNameIsLoginName: field(body, 'displayNameIsLoginName') === true,
  }
}

/** The member's own profile. */
export const profileQuery = queryOptions({
  queryKey: profileQueryKey,
  queryFn: async () => parseProfile(await apiJson(path)),
  staleTime: 0,
})

/** Whether the member has a pseudonym, so can sign a recommendation with it (C3). */
export function hasPseudonym(profile: Pick<Profile, 'pseudonym'>): boolean {
  return profile.pseudonym !== ''
}

/** A refused profile update: the server's code, and for a name error the field it is about. */
export class ProfileError extends Error {
  readonly status: number
  readonly code: string | null
  readonly field: 'displayName' | 'pseudonym' | null
  readonly retryAfter: number | null

  constructor(status: number, code: string | null, fieldName: unknown, retryAfter: number | null) {
    super(`${ProfilePath}: HTTP ${String(status)}${code ? ` (${code})` : ''}`)
    this.name = 'ProfileError'
    this.status = status
    this.code = code
    this.field = fieldName === 'displayName' || fieldName === 'pseudonym' ? fieldName : null
    this.retryAfter = retryAfter
  }
}

/** POSTs the update and returns the profile as saved; throws ProfileError when refused. */
export async function saveProfile(update: ProfileUpdate): Promise<Profile> {
  const response = await apiFetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(update),
  })
  const body: unknown = isJson(response) ? await response.json().catch(() => null) : null
  if (!response.ok) {
    const code = field(body, 'code')
    throw new ProfileError(
      response.status,
      typeof code === 'string' ? code : null,
      field(field(body, 'data'), 'field'),
      retryAfterSeconds(response),
    )
  }
  return parseProfile(body)
}

/** Whether a link typed into the form looks acceptable: an absolute https URL with a host, not too long. */
export function isValidLinkURL(url: string): boolean {
  const trimmed = url.trim()
  if (trimmed === '' || Array.from(trimmed).length > MaxExternalLinkURLLength || /\s/u.test(trimmed)) {
    return false
  }
  try {
    const parsed = new URL(trimmed)
    return parsed.protocol === 'https:' && parsed.hostname !== '' && parsed.username === '' && parsed.password === ''
  } catch {
    return false
  }
}

/** Whether a link's label is acceptable: at most 64 characters (it may be empty). */
export function isValidLinkLabel(label: string): boolean {
  return Array.from(label.trim()).length <= MaxExternalLinkLabelLength && !/[\p{Cc}\p{Cf}]/u.test(label)
}

export { MaxExternalLinks }
