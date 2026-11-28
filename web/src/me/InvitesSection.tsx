import { useQuery } from '@tanstack/react-query'
import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { InviteReasonCooldown, InviteReasonDisabled } from '@/api/types.gen'
import { InviteManager } from '@/admin/InvitesAdmin'
import { Button } from '@/components/ui/button'
import { daysLeft, fetchInviteStatus, inviteStatusQueryKey } from '@/invites/invites'
import { adminPath } from '@/lib/paths'
import { useSession } from '@/session/useSession'

/**
 * The member's own invites on the "me" page (D2, D12): during the cooling-off period the days left and a disabled
 * create button, the same when a maintainer switched the permission off; otherwise the create form and the list of
 * the member's own invites, as on the admin page (InviteManager), within the limit of active invites. Maintainers may
 * always invite and manage every invite on the admin page.
 */
export function InvitesSection() {
  const { t } = useTranslation('invites')
  const headingId = useId()
  const session = useSession()
  const maintainer = session.status === 'signed-in' && session.me.role === 'maintainer'
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-3">
      <h2 id={headingId} className="text-lg font-medium">
        {t('title')}
      </h2>
      {maintainer ? <MaintainerNote /> : <MemberInvites />}
    </section>
  )
}

function MaintainerNote() {
  const { t } = useTranslation('invites')
  return (
    <>
      <p className="text-sm text-muted-foreground">{t('me.maintainer')}</p>
      <Button variant="outline" asChild className="self-start">
        <Link to={adminPath('invites')}>{t('me.toAdmin')}</Link>
      </Button>
    </>
  )
}

function MemberInvites() {
  const { t, i18n } = useTranslation(['invites', 'common'])
  const status = useQuery({ queryKey: inviteStatusQueryKey, queryFn: fetchInviteStatus })
  if (status.isPending) {
    return (
      <p role="status" className="text-sm text-muted-foreground">
        {t('common:app.loading')}
      </p>
    )
  }
  if (status.isError) {
    return (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p className="text-sm">{t('me.loadError')}</p>
        <Button
          variant="outline"
          onClick={() => {
            void status.refetch()
          }}
        >
          {t('retry')}
        </Button>
      </div>
    )
  }
  const s = status.data
  const atLimit = s.maxActiveInvites !== null && s.activeInvites >= s.maxActiveInvites
  let note: string
  if (s.reason === InviteReasonDisabled) {
    note = t('me.disabled')
  } else if (s.reason === InviteReasonCooldown && s.cooldownEndsAt) {
    note = t('me.cooldown', {
      count: Math.max(1, daysLeft(s.cooldownEndsAt)),
      date: new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(s.cooldownEndsAt)),
    })
  } else if (atLimit) {
    note = t('me.limitReached', { count: s.maxActiveInvites ?? 0 })
  } else {
    note = t('me.allowed', { count: s.maxActiveInvites ?? 0, active: s.activeInvites })
  }
  return (
    <>
      <p role="status" className="text-sm text-muted-foreground">
        {note}
      </p>
      <InviteManager canCreate={s.allowed && !atLimit} />
    </>
  )
}
