import type { TFunction } from 'i18next'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import {
  CodeAlreadyHidden,
  CodeAlreadyReported,
  CodeNotHidden,
  CodeOwnRecommendation,
  MaxModerationReasonLength,
} from '@/api/types.gen'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { errorCode, errorRetryAfter } from '@/credentials/api'
import { rateLimitedCode } from '@/lib/authApi'
import { adminPath } from '@/lib/paths'
import { rateLimitMessage } from '@/lib/rateLimit'
import { invalidateAfterModeration } from '@/reports/invalidate'
import { ReportForm } from '@/reports/ReportForm'
import { hideRecommendation, type ReportReason, reportRecommendation, unhideRecommendation } from '@/reports/reportsApi'
import { useSession } from '@/session/useSession'
import type { RecommendationSlotProps } from './types'

/**
 * Reports and hiding on one recommendation of the card (T036; C6, D11):
 *
 * - members other than its author report it (a reason, and a note when the reason is "other"); who reported is never
 *   shown to anyone;
 * - maintainers hide it with a reason, or unhide it; there is no delete for maintainers on the card: deleting a
 *   violation is done in the review at /admin/reports (H6, M2-Q9);
 * - its author, when it is hidden, reads what that means (the card itself shows "hidden", the kind and the reason).
 */
export function ReportRecommendation({ place, recommendation: rec }: RecommendationSlotProps) {
  const session = useSession()
  if (session.status !== 'signed-in') {
    return null
  }
  const maintainer = session.me.role === 'maintainer'
  return (
    <>
      {rec.isMine && rec.hidden && <AuthorExplanation kind={rec.hidden.kind} />}
      {maintainer ? (
        <Moderate place={place.uuid} rec={rec} />
      ) : (
        !rec.isMine && <Report place={place.uuid} uuid={rec.uuid} />
      )}
    </>
  )
}

/** What a hidden recommendation means for its author, for the kinds the author can act on. */
function AuthorExplanation({ kind }: { kind: string }) {
  const { t } = useTranslation('reports')
  if (kind !== 'maintainer' && kind !== 'reports') {
    return null
  }
  return <p className="basis-full text-muted-foreground">{t(`author.${kind}`)}</p>
}

function reportError(t: TFunction<'reports'>, error: unknown): string {
  switch (errorCode(error)) {
    case CodeAlreadyReported:
      return t('report.errors.already')
    case CodeOwnRecommendation:
      return t('report.errors.own')
    case 'not_found':
      return t('report.errors.notFound')
    case rateLimitedCode:
      return rateLimitMessage(errorRetryAfter(error))
    default:
      return t('report.errors.generic')
  }
}

function Report({ place, uuid }: { place: string; uuid: string }) {
  const { t } = useTranslation('reports')
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [done, setDone] = useState(false)
  const send = useMutation({
    mutationFn: ({ reason, note }: { reason: ReportReason; note: string }) => reportRecommendation(uuid, reason, note),
    onSuccess: () => {
      setOpen(false)
      setDone(true)
      invalidateAfterModeration(queryClient, place)
    },
  })
  if (done) {
    return (
      <p role="status" className="basis-full text-muted-foreground">
        {t('report.done')}
      </p>
    )
  }
  if (!open) {
    return (
      <Button
        variant="ghost"
        size="sm"
        onClick={() => {
          send.reset()
          setOpen(true)
        }}
      >
        {t('report.action')}
      </Button>
    )
  }
  return (
    <ReportForm
      pending={send.isPending}
      error={send.isError ? reportError(t, send.error) : null}
      onSubmit={(reason, note) => {
        send.mutate({ reason, note })
      }}
      onCancel={() => {
        setOpen(false)
      }}
    />
  )
}

function moderationError(t: TFunction<'reports'>, error: unknown): string {
  switch (errorCode(error)) {
    case CodeAlreadyHidden:
      return t('moderate.errors.alreadyHidden')
    case CodeNotHidden:
      return t('moderate.errors.notHidden')
    case 'not_found':
      return t('moderate.errors.notFound')
    case rateLimitedCode:
      return rateLimitMessage(errorRetryAfter(error))
    default:
      return t('moderate.errors.generic')
  }
}

/** A maintainer's hide (with a reason) or unhide of the recommendation. */
function Moderate({ place, rec }: { place: string; rec: RecommendationSlotProps['recommendation'] }) {
  const { t } = useTranslation('reports')
  const queryClient = useQueryClient()
  const headingId = useId()
  const reasonId = useId()
  const [open, setOpen] = useState(false)
  const [reason, setReason] = useState('')
  const [message, setMessage] = useState<string | null>(null)
  const hidden = rec.hidden
  const change = useMutation({
    mutationFn: () => (hidden ? unhideRecommendation(rec.uuid) : hideRecommendation(rec.uuid, reason.trim())),
    onSuccess: (view) => {
      setOpen(false)
      setReason('')
      if (!hidden) {
        setMessage(t('moderate.hidden'))
      } else {
        setMessage(view.hidden ? t('moderate.stillHidden') : t('moderate.unhidden'))
      }
      invalidateAfterModeration(queryClient, place)
    },
  })
  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (hidden || reason.trim() !== '') {
      change.mutate()
    }
  }
  return (
    <>
      {message && (
        <p role="status" className="basis-full text-muted-foreground">
          {message}
        </p>
      )}
      {!open && (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            change.reset()
            setMessage(null)
            setOpen(true)
          }}
        >
          {hidden ? t('moderate.unhide') : t('moderate.hide')}
        </Button>
      )}
      {hidden && (
        <Button variant="link" size="sm" asChild>
          <Link to={adminPath('reports')}>{t('moderate.review')}</Link>
        </Button>
      )}
      {open && (
        <form
          aria-labelledby={headingId}
          className="flex basis-full flex-col gap-3 rounded-lg border bg-muted/40 p-3"
          onSubmit={submit}
        >
          <h4 id={headingId} className="font-medium">
            {hidden ? t('moderate.unhideTitle') : t('moderate.hideTitle')}
          </h4>
          <p className="text-muted-foreground">
            {hidden
              ? hidden.kind === 'member_removed'
                ? t('moderate.unhideMemberRemoved')
                : t('moderate.unhideIntro')
              : t('moderate.hideIntro')}
          </p>
          {change.isError && (
            <p role="alert" className="text-destructive">
              {moderationError(t, change.error)}
            </p>
          )}
          {!hidden && (
            <div className="flex flex-col gap-1">
              <label htmlFor={reasonId} className="font-medium">
                {t('moderate.reason')}
              </label>
              <Textarea
                id={reasonId}
                value={reason}
                required
                maxLength={MaxModerationReasonLength}
                onChange={(e) => {
                  setReason(e.target.value)
                }}
              />
            </div>
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              type="submit"
              variant={hidden ? 'default' : 'destructive'}
              disabled={change.isPending || (!hidden && reason.trim() === '')}
            >
              {hidden ? t('moderate.confirmUnhide') : t('moderate.confirmHide')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setOpen(false)
              }}
            >
              {t('moderate.cancel')}
            </Button>
          </div>
        </form>
      )}
    </>
  )
}
