import { useQuery } from '@tanstack/react-query'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { isNoticeKind, type Notice, noticeLink, noticesQuery, useMarkNoticesRead } from '@/me/noticesApi'
import { cn } from '@/lib/utils'

/**
 * The member's notices at the top of the "me" page (M2-Q14): newest first, unread ones marked. A notice whose
 * recommendation still exists links to it (its revisions after a maintainer's edit, otherwise the place's card) and is
 * marked read when opened; one whose recommendation is gone shows a generic text and a button to mark it read. No
 * notice says who did it: the server does not send it.
 */
export function NoticesSection() {
  const { t } = useTranslation(['notices', 'common'])
  const headingId = useId()
  const notices = useQuery(noticesQuery)
  const mark = useMarkNoticesRead()
  const unread = notices.data?.unread ?? 0
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <h2 id={headingId} className="text-lg font-medium">
          {t('title')}
        </h2>
        {unread > 0 && (
          <Button
            variant="outline"
            size="sm"
            disabled={mark.isPending}
            onClick={() => {
              mark.mutate({ all: true })
            }}
          >
            {t('markAllRead')}
          </Button>
        )}
      </div>
      {notices.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('common:app.loading')}
        </p>
      ) : notices.isError ? (
        // Not an alert: the section loads with the page, and a failure here should not interrupt what the member
        // came to the page for (a screen reader finds it in place).
        <div className="flex flex-col items-start gap-3">
          <p className="text-sm">{t('loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void notices.refetch()
            }}
          >
            {t('retry')}
          </Button>
        </div>
      ) : notices.data.notices.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('empty')}</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {notices.data.notices.map((n) => (
            <NoticeItem
              key={n.uuid}
              notice={n}
              markRead={() => {
                if (!n.read) {
                  mark.mutate({ uuids: [n.uuid] })
                }
              }}
              marking={mark.isPending}
            />
          ))}
        </ul>
      )}
      {mark.isError && (
        <p role="alert" className="text-sm text-destructive">
          {t('markError')}
        </p>
      )}
    </section>
  )
}

/** The text of a notice by its kind (a generic one for a kind the app does not know), with or without the place. */
function useNoticeText(notice: Pick<Notice, 'kind' | 'target'>): string {
  const { t } = useTranslation('notices')
  const kind = isNoticeKind(notice.kind) ? notice.kind : 'other'
  return notice.target ? t(`kind.${kind}.target`, { place: notice.target.placeName }) : t(`kind.${kind}.gone`)
}

function NoticeItem({ notice, markRead, marking }: { notice: Notice; markRead: () => void; marking: boolean }) {
  const { t, i18n } = useTranslation('notices')
  const text = useNoticeText(notice)
  const link = noticeLink(notice)
  const when = new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(notice.createdAt),
  )
  const body = (
    <>
      <span className="flex items-center gap-2 text-xs text-muted-foreground">
        {!notice.read && (
          <span className="rounded-full bg-primary px-1.5 py-0.5 font-medium text-primary-foreground">
            {t('unread')}
          </span>
        )}
        <time dateTime={notice.createdAt}>{when}</time>
      </span>
      <span className="text-sm">{text}</span>
    </>
  )
  return (
    <li
      className={cn('rounded-lg border p-3', !notice.read && 'border-primary/40 bg-primary/5')}
      data-read={notice.read}
    >
      {link ? (
        <Link
          to={link}
          onClick={markRead}
          className="flex flex-col gap-1 outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          {body}
        </Link>
      ) : (
        <div className="flex flex-col items-start gap-1">
          {body}
          {!notice.read && (
            <Button variant="link" size="sm" className="h-auto p-0" disabled={marking} onClick={markRead}>
              {t('markRead')}
            </Button>
          )}
        </div>
      )}
    </li>
  )
}
