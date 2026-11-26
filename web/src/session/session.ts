import { queryOptions } from '@tanstack/react-query'
import { ApiError, apiFetch, isJson } from '@/lib/api'

/**
 * The signed-in member as GET /api/osfm/me returns it. This is the shape the frontend assumes until the endpoint
 * exists (T007/T009); the types generated from the server's structs replace it then.
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
  // 401: no valid session. Until the endpoint exists (T007), nobody can be signed in, and the server answers either
  // 404 or, because its single-page-app fallback still catches unknown GET paths under /api/, index.html with 200.
  // Remove the last two cases once /api/osfm/me is served.
  if (response.status === 401 || response.status === 404 || (response.ok && !isJson(response))) {
    return null
  }
  if (!response.ok) {
    throw new ApiError(response.status, '/me')
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
