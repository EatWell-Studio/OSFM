// The message for too many attempts (HTTP 429 with the code rate_limited, docs/architecture.md §3.3): the server says
// in Retry-After how many seconds to wait; the pages show it in whole minutes.

import { i18n } from '@/i18n'

/** The minutes to show for a wait of seconds: rounded up, at least one. */
export function retryAfterMinutes(seconds: number): number {
  return Math.max(1, Math.ceil(seconds / 60))
}

/** "Too many attempts, try again in N minutes", or without a number when the wait is unknown. */
export function rateLimitMessage(retryAfter: number | null): string {
  return retryAfter === null
    ? i18n.t('rateLimit.later')
    : i18n.t('rateLimit.wait', { count: retryAfterMinutes(retryAfter) })
}
