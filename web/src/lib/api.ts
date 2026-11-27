// Client for the OSFM API (docs/architecture.md §3.2, §3.3, AD6). Requests go to the same origin only; the session
// lives in an HttpOnly cookie that the browser sends and the app never sees.

export const apiBase = '/api/osfm'

const safeMethods = new Set(['GET', 'HEAD', 'OPTIONS'])

/** A response with an unsuccessful HTTP status. */
export class ApiError extends Error {
  readonly status: number
  /** For a 429: the seconds to wait, from the Retry-After header; otherwise null. */
  readonly retryAfter: number | null

  constructor(status: number, path: string, retryAfter: number | null = null) {
    super(`${path}: HTTP ${String(status)}`)
    this.name = 'ApiError'
    this.status = status
    this.retryAfter = retryAfter
  }
}

/** The seconds a Retry-After header asks to wait (the server sends whole seconds), or null without a usable one. */
export function retryAfterSeconds(response: Response): number | null {
  const value = response.headers.get('Retry-After')?.trim() ?? ''
  return /^\d+$/.test(value) ? Number(value) : null
}

/**
 * Sends a request to the OSFM API. path is relative to /api/osfm and starts with "/". The session cookie is always
 * sent; state-changing requests carry the X-OSFM header that the server requires against CSRF.
 */
export function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  if (!path.startsWith('/')) {
    throw new Error(`API path must start with "/": ${path}`)
  }
  const method = (init.method ?? 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (!headers.has('Accept')) {
    headers.set('Accept', 'application/json')
  }
  if (!safeMethods.has(method)) {
    headers.set('X-OSFM', '1')
  }
  return fetch(apiBase + path, {
    ...init,
    method,
    headers,
    // The API is same-origin, so "same-origin" sends the cookie while never sending it anywhere else.
    credentials: 'same-origin',
  })
}

/** Whether the response says it is JSON. */
export function isJson(response: Response): boolean {
  const type = response.headers.get('Content-Type') ?? ''
  return /^application\/([\w.+-]+\+)?json\s*(;|$)/i.test(type)
}

/**
 * Sends a request and returns the parsed JSON body; throws ApiError for an unsuccessful status, and an Error for a
 * successful response that is not JSON.
 */
export async function apiJson(path: string, init?: RequestInit): Promise<unknown> {
  const response = await apiFetch(path, init)
  if (!response.ok) {
    throw new ApiError(response.status, path, retryAfterSeconds(response))
  }
  if (!isJson(response)) {
    throw new Error(`${path}: not a JSON response`)
  }
  return (await response.json()) as unknown
}
