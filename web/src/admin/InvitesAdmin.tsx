import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { useId, useState } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { MaxActiveInvites } from '@/api/types.gen'
import { Button } from '@/components/ui/button'
import {
  createInvite,
  defaultInviteDays,
  fetchInvites,
  type Invite,
  inviteDayChoices,
  inviteLimitReachedCode,
  inviteLink,
  inviteNotAllowedCode,
  invitesQueryKey,
  revokeInvite,
} from '@/invites/invites'
import { ShownOnceLink } from '@/components/ShownOnceLink'
import { authErrorCode, rateLimitedCode } from '@/lib/authApi'

/**
 * The invites (D1, D2), /admin/invites: creating an invite link with a validity, shown once, and the list of invites
 * with their state, to revoke the active ones. A maintainer sees every invite.
 */
export function InvitesAdmin() {
  const { t } = useTranslation('invites')
  const headingId = useId()
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4 pb-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('title')}
      </h2>
      <InviteManager canCreate />
    </section>
  )
}

/**
 * Creating invites and the list of invites, for the admin page and for a member's own invites on the "me" page (D12):
 * the list is what the server gives the caller (every invite for a maintainer, their own for a member). Without
 * canCreate the create button is disabled (a member in the cooling-off period, without the permission, or at the
 * limit of active invites).
 */
export function InviteManager({ canCreate }: { canCreate: boolean }) {
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
    },
    // Also the invite status after a refusal: the server knows better whether the member may invite.
    onSettled: () => {
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
    <>
      <div className="flex flex-wrap items-end gap-3">
        <label htmlFor={daysId} className="flex flex-col gap-1 text-sm">
          {t('validity')}
          <select
            id={daysId}
            value={days}
            disabled={!canCreate}
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
          disabled={!canCreate || create.isPending}
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
          {createErrorMessage(t, create.error)}
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
    </>
  )
}

function createErrorMessage(t: TFunction<['invites', 'common']>, error: unknown): string {
  switch (authErrorCode(error)) {
    case inviteNotAllowedCode:
      return t('errors.notAllowed')
    case inviteLimitReachedCode:
      return t('errors.limit', { count: MaxActiveInvites })
    case rateLimitedCode:
      return t('errors.rateLimited')
    default:
      return t('createError')
  }
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
