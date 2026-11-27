// The `next` parameter of the sign-in page: where to return after signing in. It comes from the URL, so anyone can
// craft it; only a path on this origin is accepted, anything else becomes the home page (no open redirect).

export const loginPath = '/login'
export const registerPath = '/register'
export const resetPath = '/reset'
export const setupPagePath = '/setup'

const fallback = '/'
const maxLength = 2048
// Any resolvable absolute base works: only whether the result stays on it matters.
const base = 'https://osfm.invalid'

function hasBackslashOrControl(s: string): boolean {
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i)
    if (c === 0x5c || c < 0x20 || c === 0x7f) {
      return true
    }
  }
  return false
}

/** Returns raw when it is a path on this origin (with its query and fragment), otherwise "/". */
export function sanitizeNext(raw: string | null | undefined): string {
  if (!raw || raw.length > maxLength) {
    return fallback
  }
  // Must be an absolute path, and not protocol-relative ("//host"). Backslashes and control characters are rejected
  // outright: browsers read "\" as "/" and drop tabs and newlines, so "/\evil" or "/\t/evil" would become "//evil".
  if (!raw.startsWith('/') || raw.startsWith('//') || hasBackslashOrControl(raw)) {
    return fallback
  }
  let url: URL
  try {
    url = new URL(raw, base)
  } catch {
    return fallback
  }
  if (url.origin !== base) {
    return fallback
  }
  // Returning to the sign-in or registration page after signing in would loop.
  if (url.pathname === loginPath || url.pathname === registerPath || url.pathname === resetPath) {
    return fallback
  }
  return url.pathname + url.search + url.hash
}

/** The sign-in page URL that returns to the given location afterwards. */
export function loginPathFor(location: { pathname: string; search: string }): string {
  const next = location.pathname + location.search
  return `${loginPath}?${new URLSearchParams({ next }).toString()}`
}
