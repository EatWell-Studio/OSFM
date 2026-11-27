import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { credentialErrorMessage } from '@/credentials/messages'
import { membersQueryKey } from './membersApi'
import { fetchResetLinks, type ResetLink, resetLinksQueryKey, revokeResetLink } from './resetLinkApi'

/**
 * Reset links (D5): every link with its state; pending ones can be revoked. Links are created from the member list
 * (Members.tsx).
 */
export function ResetLinks() {
  const { t } = useTranslation(['members', 'invites'])
  const queryClient = useQueryClient()
  const headingId = useId()
  const [error, setError] = useState<string | null>(null)
  const links = useQuery({ queryKey: resetLinksQueryKey, queryFn: fetchResetLinks })

  const revoke = useMutation({
    mutationFn: revokeResetLink,
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: resetLinksQueryKey })
      void queryClient.invalidateQueries({ queryKey: membersQueryKey })
    },
    onError: (err) => {
      setError(credentialErrorMessage(err))
    },
  })

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('reset.title')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('reset.intro')}</p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {links.isError && <p role="alert">{t('reset.loadError')}</p>}
      {links.data?.length === 0 && <p className="text-muted-foreground">{t('reset.empty')}</p>}
      {links.data && links.data.length > 0 && (
        <ul aria-label={t('reset.title')} className="flex flex-col divide-y rounded-lg border">
          {links.data.map((link) => (
            <ResetLinkRow
              key={link.uuid}
              link={link}
              revoking={revoke.isPending && revoke.variables === link.uuid}
              onRevoke={() => {
                revoke.mutate(link.uuid)
              }}
            />
          ))}
        </ul>
      )}
    </section>
  )
}

function ResetLinkRow({ link, revoking, onRevoke }: { link: ResetLink; revoking: boolean; onRevoke: () => void }) {
  const { t, i18n } = useTranslation(['members', 'invites'])
  const date = (iso: string) =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))
  const unknown = t('invites:unknownMember')
  return (
    <li className="flex flex-col gap-1 p-3 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">
          {t('reset.for', { name: link.member?.displayName ?? unknown })}
          {' · '}
          {t(`invites:state.${link.state}`)}
        </span>
        {link.state === 'active' && (
          <Button variant="destructive" size="sm" disabled={revoking} onClick={onRevoke}>
            {t('invites:revoke')}
          </Button>
        )}
      </div>
      <span className="text-muted-foreground">
        {t('invites:createdBy', {
          name: link.createdBy?.displayName ?? unknown,
          date: date(link.createdAt),
        })}
      </span>
      <span className="text-muted-foreground">
        {link.usedAt
          ? t('reset.usedAt', { date: date(link.usedAt) })
          : link.revokedAt
            ? t('invites:revokedAt', { date: date(link.revokedAt) })
            : t('invites:expiresAt', { date: date(link.expiresAt) })}
      </span>
    </li>
  )
}
