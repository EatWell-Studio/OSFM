import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus } from 'lucide-react'
import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { ShownOnceLink } from '@/components/ShownOnceLink'
import { Button } from '@/components/ui/button'
import { credentialsQueryKey, errorCode, fetchCredentials } from '@/credentials/api'
import { credentialErrorMessage } from '@/credentials/messages'
import { useStepUp } from '@/credentials/useStepUp'
import { normalizeLoginName } from '@/lib/passwordTotp'
import {
  createResetLink,
  fetchResetLinks,
  type ResetLink,
  resetLinksQueryKey,
  resetLinkUrl,
  revokeResetLink,
} from './resetLinkApi'

/**
 * Reset links (D5): a maintainer creates a one-time link for a member who lost every credential (by login name; it
 * needs a recent re-verification, since it hands over the account), shown once with a copy button; the list shows
 * every link with its state, and pending ones can be revoked.
 */
export function ResetLinks() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const headingId = useId()
  const [loginName, setLoginName] = useState('')
  const [created, setCreated] = useState<{ uuid: string; link: string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const credentials = useQuery({ queryKey: credentialsQueryKey, queryFn: fetchCredentials })
  const stepUp = useStepUp(credentials.data)
  const links = useQuery({ queryKey: resetLinksQueryKey, queryFn: fetchResetLinks })

  const create = useMutation({
    mutationFn: () => createResetLink(normalizeLoginName(loginName)),
    onSuccess: ({ resetLink, token }) => {
      setCreated({ uuid: resetLink.uuid, link: resetLinkUrl(window.location.origin, token) })
      setLoginName('')
      void queryClient.invalidateQueries({ queryKey: resetLinksQueryKey })
    },
    onError: (err) => {
      if (stepUp.handleError(err)) {
        return
      }
      switch (errorCode(err)) {
        case 'member_not_found':
          setError(t('pages.admin.reset.errors.notFound'))
          break
        case 'reset_self':
          setError(t('pages.admin.reset.errors.self'))
          break
        default:
          setError(credentialErrorMessage(t, err))
      }
    },
  })
  const revoke = useMutation({
    mutationFn: revokeResetLink,
    onSuccess: (link) => {
      if (created?.uuid === link.uuid) {
        setCreated(null)
      }
      void queryClient.invalidateQueries({ queryKey: resetLinksQueryKey })
    },
    onError: (err) => {
      setError(credentialErrorMessage(t, err))
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (loginName.trim() === '') {
      setError(t('pages.admin.reset.errors.missing'))
      return
    }
    setError(null)
    setCreated(null)
    stepUp.guard(() => {
      create.mutate()
    })
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('pages.admin.reset.title')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('pages.admin.reset.intro')}</p>
      {stepUp.panel}
      <form onSubmit={submit} noValidate className="flex flex-col gap-3">
        <Field
          label={t('pages.admin.reset.loginName')}
          name="reset-login-name"
          autoComplete="off"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          value={loginName}
          onChange={(e) => {
            setLoginName(e.target.value)
          }}
        />
        <Button type="submit" disabled={create.isPending} className="self-start">
          <Plus aria-hidden="true" />
          {t('pages.admin.reset.create')}
        </Button>
      </form>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {created && (
        <ShownOnceLink
          link={created.link}
          label={t('pages.admin.reset.link')}
          note={t('pages.admin.reset.shownOnce')}
        />
      )}
      {links.isError && <p role="alert">{t('pages.admin.reset.loadError')}</p>}
      {links.data?.length === 0 && <p className="text-muted-foreground">{t('pages.admin.reset.empty')}</p>}
      {links.data && links.data.length > 0 && (
        <ul aria-label={t('pages.admin.reset.title')} className="flex flex-col divide-y rounded-lg border">
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
  const { t, i18n } = useTranslation()
  const date = (iso: string) =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))
  const unknown = t('pages.admin.invites.unknownMember')
  return (
    <li className="flex flex-col gap-1 p-3 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">
          {t('pages.admin.reset.for', { name: link.member?.displayName ?? unknown })}
          {' · '}
          {t(`pages.admin.invites.state.${link.state}`)}
        </span>
        {link.state === 'active' && (
          <Button variant="destructive" size="sm" disabled={revoking} onClick={onRevoke}>
            {t('pages.admin.invites.revoke')}
          </Button>
        )}
      </div>
      <span className="text-muted-foreground">
        {t('pages.admin.invites.createdBy', {
          name: link.createdBy?.displayName ?? unknown,
          date: date(link.createdAt),
        })}
      </span>
      <span className="text-muted-foreground">
        {link.usedAt
          ? t('pages.admin.reset.usedAt', { date: date(link.usedAt) })
          : link.revokedAt
            ? t('pages.admin.invites.revokedAt', { date: date(link.revokedAt) })
            : t('pages.admin.invites.expiresAt', { date: date(link.expiresAt) })}
      </span>
    </li>
  )
}
