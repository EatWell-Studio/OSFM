import { queryOptions } from '@tanstack/react-query'
import { ApiError, apiFetch, isJson } from '@/lib/api'

/**
 * The signed-in member as GET /api/osfm/me returns it (auth.Me on the server). Hand-written until the types are
 * generated from the server's structs.
 */
export interface Me {
  uuid: string
  displayName: string
  role: 'member' | 'maintainer'
}

export const sessionQueryKey = ['session'] as const

function isMe(value: unknown): value is Me {
  if (typeof value !== 'object' || value === null) {
    return false
  }
  const v = value as Record<string, unknown>
  return (
    typeof v.uuid === 'string' && typeof v.displayName === 'string' && (v.role === 'member' || v.role === 'maintainer')
  )
}

/** The current member, or null when nobody is signed in. */
export async function fetchSession(): Promise<Me | null> {
  const response = await apiFetch('/me')
  // 401: no valid session. Anything else that is not a JSON 200 is an error (the server answers every /api/ path
  // with JSON, so HTML here means something in between, such as a captive portal).
  if (response.status === 401) {
    return null
  }
  if (!response.ok) {
    throw new ApiError(response.status, '/me')
  }
  if (!isJson(response)) {
    throw new Error('/me: not a JSON response')
  }
  const body: unknown = await response.json()
  if (!isMe(body)) {
    throw new Error('/me: unexpected response')
  }
  return body
}

export const sessionQuery = queryOptions({
  queryKey: sessionQueryKey,
  queryFn: fetchSession,
  staleTime: 5 * 60 * 1000,
  retry: 1,
})
