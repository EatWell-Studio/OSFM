// Calls to the public registration and sign-in endpoints (docs/architecture.md §3.3). Their error responses carry a
// machine-readable `code` (auth.AuthError on the server), which the forms turn into a message.

import { apiFetch, isJson } from '@/lib/api'
import type { Me } from '@/session/session'

/** An unsuccessful response of an auth endpoint, with the server's error code when it sent one. */
export class AuthApiError extends Error {
  readonly status: number
  readonly code: string | null

  constructor(status: number, code: string | null, path: string) {
    super(`${path}: HTTP ${String(status)}${code ? ` (${code})` : ''}`)
    this.name = 'AuthApiError'
    this.status = status
    this.code = code
  }
}

/** The error code of a failed auth request, or null for anything else (a network error, a 500). */
export function authErrorCode(error: unknown): string | null {
  return error instanceof AuthApiError ? error.code : null
}

/** A property of a parsed JSON body, or undefined. */
export function field(body: unknown, name: string): unknown {
  return typeof body === 'object' && body !== null ? (body as Record<string, unknown>)[name] : undefined
}

/** Whether a value has the shape of GET /api/osfm/me (auth.Me). */
export function isMe(value: unknown): value is Me {
  const role = field(value, 'role')
  return (
    typeof field(value, 'uuid') === 'string' &&
    typeof field(value, 'displayName') === 'string' &&
    (role === 'member' || role === 'maintainer')
  )
}

/** POSTs a JSON body to an auth endpoint and returns the parsed response; throws AuthApiError when unsuccessful. */
export async function postAuth(path: string, body: unknown): Promise<unknown> {
  const response = await apiFetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const parsed: unknown = isJson(response) ? await response.json().catch(() => null) : null
  if (!response.ok) {
    const code = field(parsed, 'code')
    throw new AuthApiError(response.status, typeof code === 'string' ? code : null, path)
  }
  if (parsed === null) {
    throw new Error(`${path}: not a JSON response`)
  }
  return parsed
}

export function unexpected(path: string): Error {
  return new Error(`${path}: unexpected response`)
}
