import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ShownOnceLink } from '@/components/ShownOnceLink'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { credentialsQueryKey, errorCode, fetchCredentials } from '@/credentials/api'
import { credentialErrorMessage } from '@/credentials/messages'
import { useStepUp } from '@/credentials/useStepUp'
import { useSession } from '@/session/useSession'
import { invitesQueryKey } from '@/invites/invites'
import { fetchMembers, type MemberInfo, membersQueryKey, openInviteEarly, setInvitePermission } from './membersApi'
import { createResetLink, resetLinksQueryKey, resetLinkUrl } from './resetLinkApi'

/**
 * The member list (maintainers only): who is in the circle, their role, when they joined and who invited them, whether
 * they may invite, how they can sign in (counts, nothing about the credentials) and whether a reset link is waiting
 * for them; members who left are listed separately. No login names. A reset link (D5) is created from a member's row:
 * it needs a recent re-verification, since it hands over the account, and is shown once with a copy button. Each row
 * of a member who is not a maintainer also switches their invite permission and, during their cooling-off period,
 * opens inviting early (D6, D12).
 */
export function Members() {
  const { t } = useTranslation(['members', 'common'])
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
          setError(t('reset.errors.notFound'))
          void queryClient.invalidateQueries({ queryKey: membersQueryKey })
          break
        case 'reset_self':
          setError(t('reset.errors.self'))
          break
        default:
          setError(credentialErrorMessage(err))
      }
    },
  })

  const inviteRightsDone = {
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: membersQueryKey })
      void queryClient.invalidateQueries({ queryKey: invitesQueryKey })
    },
    onError: () => {
      setError(t('invites.error'))
    },
  }
  const permission = useMutation({
    mutationFn: ({ member, canInvite }: { member: MemberInfo; canInvite: boolean }) =>
      setInvitePermission(member.uuid, canInvite),
    ...inviteRightsDone,
  })
  const early = useMutation({
    mutationFn: (member: MemberInfo) => openInviteEarly(member.uuid),
    ...inviteRightsDone,
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
        {t('title')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('intro')}</p>
      {stepUp.panel}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {created && (
        <div className="flex flex-col gap-2">
          <p className="text-sm">{t('reset.for', { name: created.name })}</p>
          <ShownOnceLink link={created.link} label={t('reset.link')} note={t('reset.shownOnce')} />
        </div>
      )}
      {members.isPending && (
        <p role="status" className="text-muted-foreground">
          {t('common:app.loading')}
        </p>
      )}
      {members.isError && (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p>{t('loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void members.refetch()
            }}
          >
            {t('common:retry')}
          </Button>
        </div>
      )}
      {current.length > 0 && (
        <ul aria-label={t('title')} className="flex flex-col divide-y rounded-lg border">
          {current.map((member) => (
            <MemberRow
              key={member.uuid}
              member={member}
              self={member.uuid === myUuid}
              creating={create.isPending && create.variables.uuid === member.uuid}
              onCreateResetLink={() => {
                createFor(member)
              }}
              invitePending={
                (permission.isPending && permission.variables.member.uuid === member.uuid) ||
                (early.isPending && early.variables.uuid === member.uuid)
              }
              onSetInvitePermission={(canInvite) => {
                setError(null)
                permission.mutate({ member, canInvite })
              }}
              onOpenInviteEarly={() => {
                setError(null)
                early.mutate(member)
              }}
            />
          ))}
        </ul>
      )}
      {left.length > 0 && (
        <>
          <h3 className="font-medium">{t('left')}</h3>
          <ul aria-label={t('left')} className="flex flex-col divide-y rounded-lg border">
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
  invitePending = false,
  onSetInvitePermission,
  onOpenInviteEarly,
}: {
  member: MemberInfo
  self: boolean
  creating: boolean
  onCreateResetLink?: () => void
  invitePending?: boolean
  onSetInvitePermission?: (canInvite: boolean) => void
  onOpenInviteEarly?: () => void
}) {
  const { t, i18n } = useTranslation(['members', 'common'])
  const day = (iso: string) => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(iso))
  const dateTime = (iso: string) =>
    new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso))
  const methods: string[] = []
  if (member.passwordTotp) {
    methods.push(t('passwordTotp'))
  }
  if (member.passkeys > 0) {
    methods.push(t('passkeys', { count: member.passkeys }))
  }
  return (
    <li className="flex flex-col gap-1 p-3 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">
          {member.displayName}
          {member.role === 'maintainer' && ` · ${t('maintainer')}`}
          {self && ` ${t('you')}`}
        </span>
        {onCreateResetLink && !self && (
          <Button variant="outline" size="sm" disabled={creating} onClick={onCreateResetLink}>
            {t('reset.create')}
          </Button>
        )}
      </div>
      <span className="text-muted-foreground">
        {member.invitedBy
          ? t('joinedInvited', { date: day(member.joinedAt), name: member.invitedBy.displayName })
          : t('joined', { date: day(member.joinedAt) })}
        {member.leftAt && ` · ${t('leftAt', { date: day(member.leftAt) })}`}
      </span>
      <span className="text-muted-foreground">
        {t('methods', {
          methods: methods.length > 0 ? methods.join(t('separator')) : t('noMethods'),
        })}
        {!onSetInvitePermission && (
          <>
            {' · '}
            {member.role === 'maintainer' || member.canInvite ? t('canInvite') : t('cannotInvite')}
          </>
        )}
      </span>
      {onSetInvitePermission && (
        <InviteRights
          member={member}
          pending={invitePending}
          onSetPermission={onSetInvitePermission}
          onOpenEarly={onOpenInviteEarly}
        />
      )}
      {member.pendingResetLink && (
        <span>{t('pendingReset', { date: dateTime(member.pendingResetLink.expiresAt) })}</span>
      )}
    </li>
  )
}

/**
 * A member's invite rights on their row: maintainers may always invite; for a member, the permission as a checkbox
 * and, while it is on and the cooling-off period runs, its end and "open early".
 */
function InviteRights({
  member,
  pending,
  onSetPermission,
  onOpenEarly,
}: {
  member: MemberInfo
  pending: boolean
  onSetPermission: (canInvite: boolean) => void
  onOpenEarly?: () => void
}) {
  const { t, i18n } = useTranslation('members')
  const id = useId()
  if (member.role === 'maintainer' || member.inviteCooldownEndsAt === null) {
    return <span className="text-muted-foreground">{t('invites.maintainer')}</span>
  }
  const end = member.inviteCooldownEndsAt
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
      <div className="flex items-center gap-2">
        <Checkbox
          id={id}
          checked={member.canInvite}
          disabled={pending}
          onCheckedChange={(checked) => {
            onSetPermission(checked === true)
          }}
        />
        <label htmlFor={id}>{t('invites.permission')}</label>
      </div>
      {member.canInvite && member.inviteCooldown && (
        <>
          <span className="text-muted-foreground">
            {t('invites.cooldownUntil', {
              date: new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(end)),
            })}
          </span>
          {onOpenEarly && (
            <Button variant="outline" size="sm" disabled={pending} onClick={onOpenEarly}>
              {t('invites.openEarly')}
            </Button>
          )}
        </>
      )}
    </div>
  )
}
