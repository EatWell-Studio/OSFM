import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import {
  CodeNotHidden,
  CodeNotUnderReview,
  MaxModerationReasonLength,
  ResolutionDeleted,
  ResolutionKeptHidden,
  ResolutionRestored,
} from '@/api/types.gen'
import { AttitudeBadge } from '@/components/AttitudeBadge'
import { SignatureName } from '@/components/SignatureName'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { credentialsQueryKey, errorCode, errorRetryAfter, fetchCredentials } from '@/credentials/api'
import { useStepUp } from '@/credentials/useStepUp'
import { rateLimitedCode } from '@/lib/authApi'
import { placePath, recommendationParam } from '@/lib/paths'
import { rateLimitMessage } from '@/lib/rateLimit'
import { isAttitude, isHiddenKind } from '@/place/format'
import { invalidateAfterModeration } from '@/reports/invalidate'
import {
  fetchReviewQueue,
  isReportReason,
  type Resolution,
  type RevealedAuthor,
  resolveReports,
  revealAuthor,
  reviewQueueQueryKey,
  type ReviewItem,
  unhideRecommendation,
} from '@/reports/reportsApi'

/**
 * The review queue, /admin/reports (maintainers only; C6, D11, H6, M2-Q7, M2-Q9): every recommendation with open
 * reports and every hidden one, with its place, the open reports per reason and their notes, never who reported. A
 * maintainer shows it again (or dismisses the reports of one that is shown), keeps it hidden, or deletes it as a
 * violation with a reason; this is the only place where a maintainer deletes someone's recommendation. The author of a
 * recommendation can be looked up (for a pseudonym) after a re-verification; every look is audited.
 */
export function ReportsAdmin() {
  const { t } = useTranslation(['reports', 'common'])
  const headingId = useId()
  const queue = useQuery({ queryKey: reviewQueueQueryKey, queryFn: fetchReviewQueue })
  const credentials = useQuery({ queryKey: credentialsQueryKey, queryFn: fetchCredentials })
  const stepUp = useStepUp(credentials.data)
  const [done, setDone] = useState<string | null>(null)

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('title')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('queue.intro')}</p>
      {stepUp.panel}
      {done && (
        <p role="status" className="text-sm">
          {done}
        </p>
      )}
      {queue.isPending && (
        <p role="status" className="text-muted-foreground">
          {t('common:app.loading')}
        </p>
      )}
      {queue.isError && (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p>{t('queue.loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void queue.refetch()
            }}
          >
            {t('common:retry')}
          </Button>
        </div>
      )}
      {queue.data?.length === 0 && <p className="text-sm text-muted-foreground">{t('queue.empty')}</p>}
      {queue.data && queue.data.length > 0 && (
        <ol aria-label={t('queue.label')} className="flex flex-col gap-3">
          {queue.data.map((item) => (
            <ReviewCard
              key={item.recommendation.uuid}
              item={item}
              guard={stepUp.guard}
              handleStepUpError={stepUp.handleError}
              onDone={(message) => {
                setDone(message)
              }}
            />
          ))}
        </ol>
      )}
    </section>
  )
}

function reviewError(t: TFunction<'reports'>, error: unknown): string {
  switch (errorCode(error)) {
    case CodeNotUnderReview:
      return t('queue.errors.notUnderReview')
    case CodeNotHidden:
      return t('queue.errors.notHidden')
    case 'not_found':
      return t('queue.errors.notFound')
    case rateLimitedCode:
      return rateLimitMessage(errorRetryAfter(error))
    default:
      return t('queue.errors.generic')
  }
}

type Action = Resolution | 'unhide'

function ReviewCard({
  item,
  guard,
  handleStepUpError,
  onDone,
}: {
  item: ReviewItem
  guard: (action: () => void) => void
  handleStepUpError: (error: unknown) => boolean
  onDone: (message: string) => void
}) {
  const { t } = useTranslation(['reports', 'place', 'common'])
  const queryClient = useQueryClient()
  const reasonId = useId()
  const rec = item.recommendation
  const hidden = rec.hidden
  const removed = hidden?.kind === 'member_removed'
  const [reason, setReason] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [author, setAuthor] = useState<RevealedAuthor | null>(null)
  const name = rec.authorLeft ? t('place:authorLeft') : rec.displayName

  const act = useMutation({
    mutationFn: async (action: Action) => {
      if (action === 'unhide') {
        const view = await unhideRecommendation(rec.uuid)
        return view.hidden ? t('queue.done.restoredStillHidden') : t('queue.done.restored')
      }
      const result = await resolveReports(rec.uuid, action, reason.trim())
      if (action === ResolutionRestored) {
        if (result.recommendation?.hidden) {
          return t('queue.done.restoredStillHidden')
        }
        return hidden ? t('queue.done.restored') : t('queue.done.dismissed')
      }
      return t(`queue.done.${action}`)
    },
    onSuccess: (message) => {
      onDone(message)
      invalidateAfterModeration(queryClient, item.place.uuid)
    },
  })
  const reveal = useMutation({
    mutationFn: () => revealAuthor(rec.uuid),
    onSuccess: (revealed) => {
      setAuthor(revealed)
    },
    onError: (err) => {
      handleStepUpError(err)
    },
  })

  return (
    <li
      aria-label={item.place.name}
      className="flex flex-col gap-2 rounded-lg border p-3 text-sm"
      data-hidden={hidden ? 'true' : undefined}
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Link
          to={{ pathname: placePath(item.place.uuid), search: `?${recommendationParam}=${rec.uuid}` }}
          className="font-medium break-words text-primary underline underline-offset-2"
        >
          {item.place.name}
        </Link>
        <span aria-hidden>·</span>
        <SignatureName name={name} />
        {isAttitude(rec.attitude) && <AttitudeBadge attitude={rec.attitude} />}
        {hidden && <Badge variant="destructive">{t('place:hidden')}</Badge>}
      </div>
      {hidden && (
        <div className="flex flex-col gap-1 text-muted-foreground">
          {isHiddenKind(hidden.kind) && <p>{t(`place:hiddenKind.${hidden.kind}`)}</p>}
          {hidden.reason && (
            <p className="break-words whitespace-pre-line">{t('place:hiddenReason', { reason: hidden.reason })}</p>
          )}
        </div>
      )}
      <p className="break-words whitespace-pre-line">{rec.text}</p>
      <p className="font-medium">
        {item.openReports > 0 ? t('queue.openReports', { count: item.openReports }) : t('queue.noOpenReports')}
      </p>
      {item.reasons.length > 0 && (
        <ul className="flex flex-wrap gap-1">
          {item.reasons.map((r) => (
            <li key={r.reason}>
              <Badge variant="outline">
                {t('queue.reasonCount', {
                  reason: isReportReason(r.reason) ? t(`reason.${r.reason}`) : r.reason,
                  count: r.count,
                })}
              </Badge>
            </li>
          ))}
        </ul>
      )}
      {item.notes.length > 0 && (
        <div className="flex flex-col gap-1">
          <span className="text-muted-foreground">{t('queue.notes')}</span>
          <ul aria-label={t('queue.notes')} className="flex flex-col gap-1">
            {item.notes.map((n, i) => (
              <li key={i} className="border-l-2 pl-2 break-words whitespace-pre-line">
                {n.note}
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="flex flex-col gap-1">
        <label htmlFor={reasonId} className="font-medium">
          {t('queue.reviewReason')}
        </label>
        <Textarea
          id={reasonId}
          value={reason}
          maxLength={MaxModerationReasonLength}
          onChange={(e) => {
            setReason(e.target.value)
          }}
        />
      </div>
      {act.isError && (
        <p role="alert" className="text-destructive">
          {reviewError(t, act.error)}
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        {hidden && !removed && (
          <Button
            disabled={act.isPending}
            onClick={() => {
              act.mutate(ResolutionRestored)
            }}
          >
            {t('queue.restore')}
          </Button>
        )}
        {removed && (
          <Button
            variant="outline"
            disabled={act.isPending}
            onClick={() => {
              act.mutate('unhide')
            }}
          >
            {t('queue.unhideRemoved')}
          </Button>
        )}
        {!hidden && (
          <Button
            variant="outline"
            disabled={act.isPending}
            onClick={() => {
              act.mutate(ResolutionRestored)
            }}
          >
            {t('queue.dismiss')}
          </Button>
        )}
        {hidden && item.openReports > 0 && (
          <Button
            variant="outline"
            disabled={act.isPending}
            onClick={() => {
              act.mutate(ResolutionKeptHidden)
            }}
          >
            {t('queue.keepHidden')}
          </Button>
        )}
        {!confirmDelete && (
          <Button
            variant="ghost"
            className="text-destructive"
            disabled={act.isPending}
            onClick={() => {
              setConfirmDelete(true)
            }}
          >
            {t('queue.delete')}
          </Button>
        )}
      </div>
      {confirmDelete && (
        <div className="flex flex-col gap-2 rounded-lg border border-destructive/40 p-2">
          <p>{t('queue.deleteWarning')}</p>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="destructive"
              disabled={act.isPending || reason.trim() === ''}
              onClick={() => {
                act.mutate(ResolutionDeleted)
              }}
            >
              {t('queue.deleteConfirm')}
            </Button>
            <Button
              variant="ghost"
              onClick={() => {
                setConfirmDelete(false)
              }}
            >
              {t('moderate.cancel')}
            </Button>
          </div>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        {author ? (
          <p role="status">
            {t(author.inCircle ? 'queue.revealed' : 'queue.revealedLeft', { name: author.displayName })}
          </p>
        ) : (
          <>
            <Button
              variant="link"
              size="sm"
              className="px-0"
              disabled={reveal.isPending}
              onClick={() => {
                guard(() => {
                  reveal.mutate()
                })
              }}
            >
              {t('queue.reveal')}
            </Button>
            <span className="text-xs text-muted-foreground">{t('queue.revealHint')}</span>
          </>
        )}
        {reveal.isError && errorCode(reveal.error) !== 'reauth_required' && (
          <p role="alert" className="text-destructive">
            {reviewError(t, reveal.error)}
          </p>
        )}
      </div>
    </li>
  )
}
