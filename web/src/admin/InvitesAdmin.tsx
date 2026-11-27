import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  createInvite,
  defaultInviteDays,
  fetchInvites,
  type Invite,
  inviteDayChoices,
  inviteLink,
  invitesQueryKey,
  revokeInvite,
} from '@/invites/invites'
import { ShownOnceLink } from '@/components/ShownOnceLink'

/**
 * The invites (D1, D2), /admin/invites: creating an invite link with a validity, shown once, and the list of invites
 * with their state, to revoke the active ones.
 */
export function InvitesAdmin() {
  const { t } = useTranslation(['invites', 'common'])
  const queryClient = useQueryClient()
  const daysId = useId()
  const [days, setDays] = useState<number>(defaultInviteDays)
  const [created, setCreated] = useState<{ uuid: string; link: string } | null>(null)

  const invites = useQuery({ queryKey: invitesQueryKey, queryFn: fetchInvites })
  const create = useMutation({
    mutationFn: () => createInvite(days),
    onSuccess: ({ invite, code }) => {
      setCreated({ uuid: invite.uuid, link: inviteLink(window.location.origin, code) })
      void queryClient.invalidateQueries({ queryKey: invitesQueryKey })
    },
  })
  const revoke = useMutation({
    mutationFn: revokeInvite,
    onSuccess: (invite) => {
      if (created?.uuid === invite.uuid) {
        setCreated(null)
      }
      void queryClient.invalidateQueries({ queryKey: invitesQueryKey })
    },
  })

  return (
    <section aria-labelledby={`${daysId}-heading`} className="flex flex-col gap-4 px-4 pb-4">
      <h2 id={`${daysId}-heading`} className="text-lg font-medium">
        {t('title')}
      </h2>
      <div className="flex flex-wrap items-end gap-3">
        <label htmlFor={daysId} className="flex flex-col gap-1 text-sm">
          {t('validity')}
          <select
            id={daysId}
            value={days}
            onChange={(e) => {
              setDays(Number(e.target.value))
            }}
            className="h-8 rounded-lg border bg-background px-2"
          >
            {inviteDayChoices.map((d) => (
              <option key={d} value={d}>
                {t('days', { count: d })}
              </option>
            ))}
          </select>
        </label>
        <Button
          disabled={create.isPending}
          onClick={() => {
            create.mutate()
          }}
        >
          <Plus aria-hidden="true" />
          {t('create')}
        </Button>
      </div>
      {create.isError && (
        <p role="alert" className="text-sm text-destructive">
          {t('createError')}
        </p>
      )}
      {created && <ShownOnceLink link={created.link} label={t('link')} note={t('shownOnce')} />}

      {invites.isPending && (
        <p role="status" className="text-muted-foreground">
          {t('common:app.loading')}
        </p>
      )}
      {invites.isError && (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p>{t('loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void invites.refetch()
            }}
          >
            {t('retry')}
          </Button>
        </div>
      )}
      {revoke.isError && (
        <p role="alert" className="text-sm text-destructive">
          {t('revokeError')}
        </p>
      )}
      {invites.data?.length === 0 && <p className="text-muted-foreground">{t('empty')}</p>}
      {invites.data && invites.data.length > 0 && (
        <ul aria-label={t('title')} className="flex flex-col divide-y rounded-lg border">
          {invites.data.map((invite) => (
            <InviteRow
              key={invite.uuid}
              invite={invite}
              revoking={revoke.isPending && revoke.variables === invite.uuid}
              onRevoke={() => {
                revoke.mutate(invite.uuid)
              }}
            />
          ))}
        </ul>
      )}
    </section>
  )
}

function InviteRow({ invite, revoking, onRevoke }: { invite: Invite; revoking: boolean; onRevoke: () => void }) {
  const { t, i18n } = useTranslation('invites')
  const date = (iso: string) =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))
  return (
    <li className="flex flex-col gap-1 p-3 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">{t(`state.${invite.state}`)}</span>
        {invite.state === 'active' && (
          <Button variant="destructive" size="sm" disabled={revoking} onClick={onRevoke}>
            {t('revoke')}
          </Button>
        )}
      </div>
      <span className="text-muted-foreground">
        {t('createdBy', {
          name: invite.createdBy?.displayName ?? t('unknownMember'),
          date: date(invite.createdAt),
        })}
      </span>
      {invite.usedAt ? (
        <span className="text-muted-foreground">
          {t('usedBy', {
            name: invite.usedBy?.displayName ?? t('unknownMember'),
            date: date(invite.usedAt),
          })}
        </span>
      ) : invite.revokedAt ? (
        <span className="text-muted-foreground">{t('revokedAt', { date: date(invite.revokedAt) })}</span>
      ) : (
        <span className="text-muted-foreground">{t('expiresAt', { date: date(invite.expiresAt) })}</span>
      )}
    </li>
  )
}
