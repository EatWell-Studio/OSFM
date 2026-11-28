// The member's notices (C5, C6, D11, M2-Q14): GET /api/osfm/me/notices and POST /api/osfm/me/notices/read
// (internal/api/notices.go). The bottom navigation shows the unread count and the "me" page the list; both read
// noticesQuery, and marking notices read puts the server's answer into it, so both update at once.

import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  type MarkNoticesRead,
  type Notice,
  NoticeKindHiddenByMaintainer,
  NoticeKindHiddenByReports,
  NoticeKindMaintainerEdited,
  NoticeKindRestored,
  NoticeKindViolationDeleted,
  type NoticeList,
  NoticesPath,
  NoticesReadPath,
  type NoticeTarget,
} from '@/api/types.gen'
import { ApiError, apiJson } from '@/lib/api'
import { field } from '@/lib/authApi'
import { isUuid, placePath, recommendationParam, revisionsPath } from '@/lib/paths'

export type { Notice, NoticeList, NoticeTarget }

/** Paths relative to the API base (apiFetch takes paths under /api/osfm). */
const listPath = NoticesPath.replace(/^\/api\/osfm/, '')
const readPath = NoticesReadPath.replace(/^\/api\/osfm/, '')

export const noticesQueryKey = ['me', 'notices'] as const

/** The kinds the app has texts for; a notice of another kind (from a newer server) gets a generic text. */
export const noticeKinds = [
  NoticeKindMaintainerEdited,
  NoticeKindHiddenByMaintainer,
  NoticeKindHiddenByReports,
  NoticeKindRestored,
  NoticeKindViolationDeleted,
] as const
export type NoticeKind = (typeof noticeKinds)[number]

export function isNoticeKind(kind: string): kind is NoticeKind {
  return (noticeKinds as readonly string[]).includes(kind)
}

function parseTarget(value: unknown): NoticeTarget | null {
  const recommendation = field(value, 'recommendation')
  const place = field(value, 'place')
  const placeName = field(value, 'placeName')
  if (
    typeof recommendation !== 'string' ||
    !isUuid(recommendation) ||
    typeof place !== 'string' ||
    !isUuid(place) ||
    typeof placeName !== 'string'
  ) {
    return null
  }
  return { recommendation, place, placeName }
}

function parseNotice(value: unknown): Notice | null {
  const uuid = field(value, 'uuid')
  const kind = field(value, 'kind')
  const createdAt = field(value, 'createdAt')
  if (typeof uuid !== 'string' || typeof kind !== 'string' || typeof createdAt !== 'string') {
    return null
  }
  return { uuid, kind, createdAt, read: field(value, 'read') === true, target: parseTarget(field(value, 'target')) }
}

/** Reads a notice list from a response body; throws when it does not have the shape of one. */
export function parseNoticeList(body: unknown): NoticeList {
  const list = field(body, 'notices')
  const unread = field(body, 'unread')
  if (!Array.isArray(list) || typeof unread !== 'number') {
    throw new Error(`${NoticesPath}: unexpected response`)
  }
  const notices = list.map(parseNotice)
  if (!notices.every((n) => n !== null)) {
    throw new Error(`${NoticesPath}: unexpected response`)
  }
  return { notices, unread }
}

/** The member's notices. Refreshed when the app comes back to the foreground and after a minute on a new page. */
export const noticesQuery = queryOptions({
  queryKey: noticesQueryKey,
  queryFn: async () => parseNoticeList(await apiJson(listPath)),
  staleTime: 60_000,
  // Once after a network or server error; an answer such as 404 (an older server) stays as it is.
  retry: (failures, error) => failures < 1 && !(error instanceof ApiError && error.status < 500),
})

/** POSTs which notices to mark read and returns the notices afterwards. */
export async function markNoticesRead(request: MarkNoticesRead): Promise<NoticeList> {
  return parseNoticeList(
    await apiJson(readPath, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
    }),
  )
}

/** The mutation that marks notices read and puts the answer into noticesQuery. */
export function useMarkNoticesRead() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: markNoticesRead,
    onSuccess: (list) => {
      client.setQueryData(noticesQueryKey, list)
    },
  })
}

/**
 * Where a notice links to: the revisions of the recommendation after a maintainer's edit, otherwise the place's card
 * pointing at the recommendation; nowhere once the recommendation is gone.
 */
export function noticeLink(notice: Pick<Notice, 'kind' | 'target'>): string | null {
  const target = notice.target
  if (!target) {
    return null
  }
  if (notice.kind === NoticeKindMaintainerEdited) {
    return revisionsPath(target.place, target.recommendation)
  }
  const query = new URLSearchParams({ [recommendationParam]: target.recommendation })
  return `${placePath(target.place)}?${query.toString()}`
}
