// The app's paths for places and recommendations, in one place so that links, redirects and shared links agree with
// the routes (routes.tsx). A place's own address is also its share link (M2-Q15): /p/<place uuid>, with ?r=<recommendation
// uuid> to point at one recommendation. No route's last segment contains a dot, and none is under /assets or /map: the
// server answers those with 404 instead of the app (docs/architecture.md §4.5).

import { useParams } from 'react-router'

/** A UUIDv7 as the server writes them (store.UUIDPattern). */
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/

export function isUuid(value: string | undefined | null): value is string {
  return typeof value === 'string' && uuidPattern.test(value)
}

/** The query parameter of a share link that names one recommendation of the place. */
export const recommendationParam = 'r'

export const listPath = '/list'
export const newPlacePath = '/new'

export const placePath = (place: string) => `/p/${place}`
export const editPlacePath = (place: string) => `/p/${place}/edit`
export const recommendPath = (place: string) => `/p/${place}/recommend`
export const editRecommendationPath = (place: string, rec: string) => `/p/${place}/r/${rec}/edit`
export const revisionsPath = (place: string, rec: string) => `/p/${place}/r/${rec}/history`

/** The link to share a place, or one of its recommendations, with the group (C7, M2-Q15). */
export function shareLink(origin: string, place: string, rec?: string): string {
  const url = new URL(placePath(place), origin)
  if (rec) {
    url.searchParams.set(recommendationParam, rec)
  }
  return url.toString()
}

/** The route patterns, relative to the signed-in layout. */
export const routePatterns = {
  list: 'list',
  newPlace: 'new',
  place: 'p/:place',
  editPlace: 'p/:place/edit',
  recommend: 'p/:place/recommend',
  editRecommendation: 'p/:place/r/:rec/edit',
  revisions: 'p/:place/r/:rec/history',
} as const

/** The administration's sections: /admin/<section>. */
export const adminSections = ['invites', 'members', 'tree', 'reports', 'settings', 'backup'] as const
export type AdminSection = (typeof adminSections)[number]
export const adminPath = (section: AdminSection) => `/admin/${section}`

/**
 * The place (and recommendation) of the current route, when they are well-formed UUIDs; null otherwise, and the page
 * shows that the address is not valid. Whether the place exists, and is visible to the member, is the server's answer.
 */
export function usePlaceParams(): { place: string; rec: string | undefined } | null {
  const { place, rec } = useParams()
  if (!isUuid(place) || (rec !== undefined && !isUuid(rec))) {
    return null
  }
  return { place, rec }
}
