import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ShownOnceLink } from '@/components/ShownOnceLink'
import { Button } from '@/components/ui/button'
import { credentialsQueryKey, errorCode, fetchCredentials } from '@/credentials/api'
import { credentialErrorMessage } from '@/credentials/messages'
import { useStepUp } from '@/credentials/useStepUp'
import { useSession } from '@/session/useSession'
import { fetchMembers, type MemberInfo, membersQueryKey } from './membersApi'
import { createResetLink, resetLinksQueryKey, resetLinkUrl } from './resetLinkApi'

/**
 * The member list (maintainers only): who is in the circle, their role, when they joined and who invited them, whether
 * they may invite, how they can sign in (counts, nothing about the credentials) and whether a reset link is waiting
 * for them; members who left are listed separately. No login names. A reset link (D5) is created from a member's row:
 * it needs a recent re-verification, since it hands over the account, and is shown once with a copy button.
 */
export function Members() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const headingId = useId()
  const session = useSession()
  const myUuid = session.status === 'signed-in' ? session.me.uuid : null
  const [created, setCreated] = useState<{ name: string; link: string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const members = useQuery({ queryKey: membersQueryKey, queryFn: fetchMembers })
  const credentials = useQuery({ queryKey: credentialsQueryKey, queryFn: fetchCredentials })
  const stepUp = useStepUp(credentials.data)

  const create = useMutation({
    mutationFn: (member: MemberInfo) => createResetLink(member.uuid),
    onSuccess: ({ token }, member) => {
      setCreated({ name: member.displayName, link: resetLinkUrl(window.location.origin, token) })
      void queryClient.invalidateQueries({ queryKey: resetLinksQueryKey })
      void queryClient.invalidateQueries({ queryKey: membersQueryKey })
    },
    onError: (err) => {
      if (stepUp.handleError(err)) {
        return
      }
      switch (errorCode(err)) {
        case 'member_not_found':
          setError(t('pages.admin.reset.errors.notFound'))
          void queryClient.invalidateQueries({ queryKey: membersQueryKey })
          break
        case 'reset_self':
          setError(t('pages.admin.reset.errors.self'))
          break
        default:
          setError(credentialErrorMessage(t, err))
      }
    },
  })

  const createFor = (member: MemberInfo) => {
    setError(null)
    setCreated(null)
    stepUp.guard(() => {
      create.mutate(member)
    })
  }

  const current = members.data?.filter((m) => m.leftAt === null) ?? []
  const left = members.data?.filter((m) => m.leftAt !== null) ?? []
  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('pages.admin.members.title')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('pages.admin.members.intro')}</p>
      {stepUp.panel}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {created && (
        <div className="flex flex-col gap-2">
          <p className="text-sm">{t('pages.admin.reset.for', { name: created.name })}</p>
          <ShownOnceLink
            link={created.link}
            label={t('pages.admin.reset.link')}
            note={t('pages.admin.reset.shownOnce')}
          />
        </div>
      )}
      {members.isPending && (
        <p role="status" className="text-muted-foreground">
          {t('app.loading')}
        </p>
      )}
      {members.isError && (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p>{t('pages.admin.members.loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void members.refetch()
            }}
          >
            {t('pages.admin.invites.retry')}
          </Button>
        </div>
      )}
      {current.length > 0 && (
        <ul aria-label={t('pages.admin.members.title')} className="flex flex-col divide-y rounded-lg border">
          {current.map((member) => (
            <MemberRow
              key={member.uuid}
              member={member}
              self={member.uuid === myUuid}
              creating={create.isPending && create.variables.uuid === member.uuid}
              onCreateResetLink={() => {
                createFor(member)
              }}
            />
          ))}
        </ul>
      )}
      {left.length > 0 && (
        <>
          <h3 className="font-medium">{t('pages.admin.members.left')}</h3>
          <ul aria-label={t('pages.admin.members.left')} className="flex flex-col divide-y rounded-lg border">
            {left.map((member) => (
              <MemberRow key={member.uuid} member={member} self={false} creating={false} />
            ))}
          </ul>
        </>
      )}
    </section>
  )
}

function MemberRow({
  member,
  self,
  creating,
  onCreateResetLink,
}: {
  member: MemberInfo
  self: boolean
  creating: boolean
  onCreateResetLink?: () => void
}) {
  const { t, i18n } = useTranslation()
  const day = (iso: string) => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(iso))
  const dateTime = (iso: string) =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))
  const methods: string[] = []
  if (member.passwordTotp) {
    methods.push(t('pages.admin.members.passwordTotp'))
  }
  if (member.passkeys > 0) {
    methods.push(t('pages.admin.members.passkeys', { count: member.passkeys }))
  }
  return (
    <li className="flex flex-col gap-1 p-3 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">
          {member.displayName}
          {member.role === 'maintainer' && ` · ${t('pages.admin.members.maintainer')}`}
          {self && ` ${t('pages.admin.members.you')}`}
        </span>
        {onCreateResetLink && !self && (
          <Button variant="outline" size="sm" disabled={creating} onClick={onCreateResetLink}>
            {t('pages.admin.reset.create')}
          </Button>
        )}
      </div>
      <span className="text-muted-foreground">
        {member.invitedBy
          ? t('pages.admin.members.joinedInvited', { date: day(member.joinedAt), name: member.invitedBy.displayName })
          : t('pages.admin.members.joined', { date: day(member.joinedAt) })}
        {member.leftAt && ` · ${t('pages.admin.members.leftAt', { date: day(member.leftAt) })}`}
      </span>
      <span className="text-muted-foreground">
        {t('pages.admin.members.methods', {
          methods:
            methods.length > 0 ? methods.join(t('pages.admin.members.separator')) : t('pages.admin.members.noMethods'),
        })}
        {' · '}
        {member.canInvite ? t('pages.admin.members.canInvite') : t('pages.admin.members.cannotInvite')}
      </span>
      {member.pendingResetLink && (
        <span>{t('pages.admin.members.pendingReset', { date: dateTime(member.pendingResetLink.expiresAt) })}</span>
      )}
    </li>
  )
}
