// Reports, hiding and their review (C6, D11, H6; M2-Q7, M2-Q8, M2-Q9): the routes of internal/api/reports.go. Members
// report a recommendation from its card; maintainers hide and unhide it there, and review the reported and hidden
// recommendations at /admin/reports. No response names who reported. Errors carry the server's code (postAuth).

import {
  HidePath,
  type HideRequest,
  type RecommendationView,
  ReportPath,
  ReportReasonAbuse,
  ReportReasonAdvertising,
  ReportReasonFalseOrDuplicate,
  ReportReasonOther,
  ReportReasonPoliticalIllegal,
  type ReportRequest,
  ReportReasonUnverifiedClaim,
  ResolutionDeleted,
  ResolutionKeptHidden,
  ResolutionRestored,
  ResolvePath,
  type ResolveRequest,
  type ResolveResult,
  RevealAuthorPath,
  type RevealedAuthor,
  ReviewQueuePath,
  type ReviewItem,
  type ReviewQueue,
  UnhidePath,
  type UnhideRequest,
} from '@/api/types.gen'
import { apiJson } from '@/lib/api'
import { field, postAuth, unexpected } from '@/lib/authApi'

export type { ReviewItem, ReviewQueue, RevealedAuthor, ResolveResult }

/** The reason codes, in the order the form lists them. */
export const reportReasons = [
  ReportReasonAdvertising,
  ReportReasonAbuse,
  ReportReasonPoliticalIllegal,
  ReportReasonUnverifiedClaim,
  ReportReasonFalseOrDuplicate,
  ReportReasonOther,
] as const
export type ReportReason = (typeof reportReasons)[number]

export function isReportReason(value: string): value is ReportReason {
  return (reportReasons as readonly string[]).includes(value)
}

export const resolutions = [ResolutionRestored, ResolutionKeptHidden, ResolutionDeleted] as const
export type Resolution = (typeof resolutions)[number]

/** A route's path relative to the API base, with its {uuid} filled in. */
function path(pattern: string, uuid: string): string {
  return pattern.replace(/^\/api\/osfm/, '').replace('{uuid}', encodeURIComponent(uuid))
}

export const reviewQueueQueryKey = ['reports', 'queue'] as const

const isString = (v: unknown): v is string => typeof v === 'string'

function isView(v: unknown): v is RecommendationView {
  return isString(field(v, 'uuid')) && isString(field(v, 'displayName')) && isString(field(v, 'text'))
}

function isItem(v: unknown): v is ReviewItem {
  const place = field(v, 'place')
  const last = field(v, 'lastReportedAt')
  return (
    isView(field(v, 'recommendation')) &&
    isString(field(place, 'uuid')) &&
    isString(field(place, 'name')) &&
    typeof field(v, 'openReports') === 'number' &&
    Array.isArray(field(v, 'reasons')) &&
    Array.isArray(field(v, 'notes')) &&
    (last === null || isString(last))
  )
}

/** POST /api/osfm/recommendations/{uuid}/report. */
export async function reportRecommendation(uuid: string, reason: ReportReason, note: string): Promise<void> {
  const request: ReportRequest = note === '' ? { reason } : { reason, note }
  await postAuth(path(ReportPath, uuid), request)
}

/** POST /api/osfm/recommendations/{uuid}/hide (maintainers): the recommendation as it is now. */
export async function hideRecommendation(uuid: string, reason: string): Promise<RecommendationView> {
  const p = path(HidePath, uuid)
  const request: HideRequest = { reason }
  const body = await postAuth(p, request)
  if (!isView(body)) {
    throw unexpected(p)
  }
  return body
}

/** POST /api/osfm/recommendations/{uuid}/unhide (maintainers). It can stay hidden as member_removed (see the server). */
export async function unhideRecommendation(uuid: string): Promise<RecommendationView> {
  const p = path(UnhidePath, uuid)
  const request: UnhideRequest = {}
  const body = await postAuth(p, request)
  if (!isView(body)) {
    throw unexpected(p)
  }
  return body
}

/** GET /api/osfm/reports (maintainers): the recommendations under review. */
export async function fetchReviewQueue(): Promise<ReviewItem[]> {
  const p = path(ReviewQueuePath, '')
  const items = field(await apiJson(p), 'items')
  if (!Array.isArray(items) || !items.every(isItem)) {
    throw unexpected(p)
  }
  return items
}

/** POST /api/osfm/reports/{uuid}/resolve (maintainers); deleted is the violation delete and needs a reason. */
export async function resolveReports(uuid: string, resolution: Resolution, reason: string): Promise<ResolveResult> {
  const p = path(ResolvePath, uuid)
  const request: ResolveRequest = reason === '' ? { resolution } : { resolution, reason }
  const body = await postAuth(p, request)
  const rec = field(body, 'recommendation')
  if (!isString(field(body, 'resolution')) || !(rec === null || isView(rec))) {
    throw unexpected(p)
  }
  return body as ResolveResult
}

/** POST /api/osfm/reports/{uuid}/author (maintainers, fresh proof; audited): the member who wrote it. */
export async function revealAuthor(uuid: string): Promise<RevealedAuthor> {
  const p = path(RevealAuthorPath, uuid)
  const body = await postAuth(p, {})
  if (
    !isString(field(body, 'uuid')) ||
    !isString(field(body, 'displayName')) ||
    typeof field(body, 'inCircle') !== 'boolean'
  ) {
    throw unexpected(p)
  }
  return body as RevealedAuthor
}
