// Members' names (main names and pseudonyms) as the forms check them before sending (M2-Q6). The server decides:
// it compares names in a normalised form (store.NameKey: NFKC, case folding, invisible characters removed, white space
// collapsed), which these checks only approximate, and it alone knows other members' names.

import { MaxNameLength } from '@/api/types.gen'

/** Characters that may not be part of a name: control and format characters (as the server refuses them). */
const forbidden = /[\p{Cc}\p{Cf}]/u

/** An approximation of the server's comparison form of a name, enough to compare it with a login name. */
export function nameKey(name: string): string {
  return name.normalize('NFKC').toLowerCase().normalize('NFKC').replace(/\s+/gu, ' ').trim()
}

/** Whether a name (trimmed) is acceptable as a main name or pseudonym: 1 to 64 characters, none of them forbidden. */
export function isValidName(name: string): boolean {
  const trimmed = name.trim()
  const length = Array.from(trimmed).length
  return length > 0 && length <= MaxNameLength && !forbidden.test(trimmed)
}

/**
 * What is wrong with a display name given at registration: 'invalid' (empty, too long, forbidden characters), 'login'
 * (the login name, in any letter case or width), or null.
 */
export function displayNameProblem(displayName: string, loginName: string): 'invalid' | 'login' | null {
  if (!isValidName(displayName)) {
    return 'invalid'
  }
  return nameKey(displayName) === nameKey(loginName) ? 'login' : null
}
