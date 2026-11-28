// Small helpers of the place card: dates in the member's language, labels of fixed values, links that may be opened.

import {
  type Attitude,
  attitudes,
  type Category,
  categories,
  type Cuisine,
  cuisines,
  type SourceKind,
  sourceKinds,
} from '@/lib/enums'

/** A moment (RFC 3339) as a date in the language, in the browser's time zone; the text as given when unreadable. */
export function formatDate(moment: string, language: string): string {
  const date = new Date(moment)
  if (Number.isNaN(date.getTime())) {
    return moment
  }
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium' }).format(date)
}

/** A calendar day (YYYY-MM-DD) as a date in the language, the same day in every time zone. */
export function formatDay(day: string, language: string): string {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) {
    return day
  }
  const date = new Date(`${day}T00:00:00Z`)
  if (Number.isNaN(date.getTime())) {
    return day
  }
  return new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeZone: 'UTC' }).format(date)
}

function oneOf<T extends string>(values: readonly T[]) {
  return (value: string | undefined): value is T => values.includes(value as T)
}

export const isAttitude = oneOf<Attitude>(attitudes)
export const isCategory = oneOf<Category>(categories)
export const isCuisine = oneOf<Cuisine>(cuisines)
export const isSourceKind = oneOf<SourceKind>(sourceKinds)

/** The statuses a place can have, with a label in the place namespace (status.<value>). */
export const placeStatuses = ['open', 'maybe_closed', 'closed'] as const
export const isPlaceStatus = oneOf(placeStatuses)

/** The ways a recommendation can be hidden, with an explanation in the place namespace (hiddenKind.<value>). */
export const hiddenKinds = ['maintainer', 'reports', 'member_removed'] as const
export const isHiddenKind = oneOf(hiddenKinds)

/**
 * A source's link, when it is an absolute http(s) URL (the server accepts nothing else; this is a backstop): the
 * address to open and a short text to show. Null for anything else, which is then shown as plain text.
 */
export function sourceLink(url: string | undefined): { href: string; text: string } | null {
  if (!url) {
    return null
  }
  let parsed: URL
  try {
    parsed = new URL(url)
  } catch {
    return null
  }
  if ((parsed.protocol !== 'https:' && parsed.protocol !== 'http:') || parsed.hostname === '') {
    return null
  }
  const path = parsed.pathname === '/' ? '' : parsed.pathname
  return { href: parsed.href, text: parsed.host + path + parsed.search }
}
