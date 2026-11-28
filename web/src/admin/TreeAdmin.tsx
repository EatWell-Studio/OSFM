import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { type BranchMember, BranchRemove, BranchStopInvites, RoleMaintainer, type TreeMember } from '@/api/types.gen'
import { Button } from '@/components/ui/button'
import { credentialsQueryKey, errorCode, fetchCredentials } from '@/credentials/api'
import { useStepUp } from '@/credentials/useStepUp'
import { useSession } from '@/session/useSession'
import { governanceErrorMessage } from './governanceErrors'
import {
  type BranchAction,
  executeBranch,
  fetchMemberTree,
  memberTreeQueryKey,
  membersQueryKey,
  previewBranch,
} from './membersApi'
import { RemovalForm, type RecommendationsChoice } from './RemovalForm'

/**
 * The invite tree, /admin/tree (maintainers only; D10): who invited whom, as a collapsible tree with each member's role,
 * dates and invite permission (no login names, no pseudonyms). A branch is a member and everyone invited below them;
 * stopping its invites or removing it first shows who the action applies to (maintainers are listed but left alone),
 * then asks for confirmation and, when the session is not freshly verified, a re-verification. When the branch has
 * changed since the preview, the server refuses and the preview is shown again.
 */
export function TreeAdmin() {
  const { t } = useTranslation(['members', 'common'])
  const headingId = useId()
  const queryClient = useQueryClient()
  const session = useSession()
  const myUuid = session.status === 'signed-in' ? session.me.uuid : null
  const tree = useQuery({ queryKey: memberTreeQueryKey, queryFn: fetchMemberTree })
  const credentials = useQuery({ queryKey: credentialsQueryKey, queryFn: fetchCredentials })
  const stepUp = useStepUp(credentials.data)
  const [branch, setBranch] = useState<{ member: TreeMember; action: BranchAction } | null>(null)
  const [done, setDone] = useState<string | null>(null)

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4 px-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('tree.title')}
      </h2>
      <p className="text-sm text-muted-foreground">{t('tree.intro')}</p>
      {stepUp.panel}
      {done && (
        <p role="status" className="text-sm">
          {done}
        </p>
      )}
      {branch && (
        <BranchPanel
          key={`${branch.member.uuid}-${branch.action}`}
          member={branch.member}
          action={branch.action}
          guard={stepUp.guard}
          handleStepUpError={stepUp.handleError}
          onDone={(count) => {
            setDone(
              t(branch.action === BranchRemove ? 'tree.removed' : 'tree.stopped', {
                count,
                name: branch.member.displayName,
              }),
            )
            setBranch(null)
            void queryClient.invalidateQueries({ queryKey: membersQueryKey })
          }}
          onCancel={() => {
            setBranch(null)
          }}
        />
      )}
      {tree.isPending && (
        <p role="status" className="text-muted-foreground">
          {t('common:app.loading')}
        </p>
      )}
      {tree.isError && (
        <div role="alert" className="flex flex-col items-start gap-3">
          <p>{t('tree.loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void tree.refetch()
            }}
          >
            {t('common:retry')}
          </Button>
        </div>
      )}
      {tree.data && (
        <ul aria-label={t('tree.title')} className="flex flex-col gap-1">
          {tree.data.map((root) => (
            <TreeNode
              key={root.uuid}
              member={root}
              myUuid={myUuid}
              onBranch={(member, action) => {
                setDone(null)
                setBranch({ member, action })
              }}
            />
          ))}
        </ul>
      )}
    </section>
  )
}

function countBelow(member: TreeMember): number {
  return member.invited.reduce((n, m) => n + 1 + countBelow(m), 0)
}

function TreeNode({
  member,
  myUuid,
  onBranch,
}: {
  member: TreeMember
  myUuid: string | null
  onBranch: (member: TreeMember, action: BranchAction) => void
}) {
  const { t, i18n } = useTranslation('members')
  const [open, setOpen] = useState(true)
  const childrenId = useId()
  const day = (iso: string) => new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(iso))
  const inCircle = member.leftAt === null
  let invite = t('cannotInvite')
  if (member.role === RoleMaintainer) {
    invite = t('canInvite')
  } else if (member.canInvite) {
    invite = member.inviteCooldown ? t('tree.cooldown') : t('canInvite')
  }
  const below = countBelow(member)
  return (
    <li className="flex flex-col gap-1">
      <div role="group" aria-label={member.displayName} className="flex flex-col gap-1 rounded-lg border p-2 text-sm">
        <div className="flex items-center gap-1">
          {member.invited.length > 0 ? (
            <Button
              variant="ghost"
              size="icon"
              aria-expanded={open}
              aria-controls={childrenId}
              aria-label={open ? t('tree.collapse') : t('tree.expand', { count: below })}
              onClick={() => {
                setOpen(!open)
              }}
            >
              {open ? <ChevronDown aria-hidden /> : <ChevronRight aria-hidden />}
            </Button>
          ) : (
            <span className="inline-block w-8" aria-hidden />
          )}
          <span className={inCircle ? 'font-medium' : 'font-medium text-muted-foreground line-through'}>
            {member.displayName}
          </span>
          {member.role === RoleMaintainer && <span>{` · ${t('maintainer')}`}</span>}
          {member.uuid === myUuid && <span>{` ${t('you')}`}</span>}
        </div>
        <span className="pl-9 text-muted-foreground">
          {t('joined', { date: day(member.joinedAt) })}
          {member.leftAt ? ` · ${t('leftAt', { date: day(member.leftAt) })}` : ` · ${invite}`}
          {below > 0 && ` · ${t('tree.below', { count: below })}`}
        </span>
        <div className="flex flex-wrap gap-2 pl-9">
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              onBranch(member, BranchStopInvites)
            }}
          >
            {t('tree.stopInvites')}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              onBranch(member, BranchRemove)
            }}
          >
            {t('tree.remove')}
          </Button>
        </div>
      </div>
      {member.invited.length > 0 && open && (
        <ul
          id={childrenId}
          aria-label={t('tree.invitedBy', { name: member.displayName })}
          className="ml-4 flex flex-col gap-1 border-l pl-2"
        >
          {member.invited.map((m) => (
            <TreeNode key={m.uuid} member={m} myUuid={myUuid} onBranch={onBranch} />
          ))}
        </ul>
      )}
    </li>
  )
}

/**
 * The preview of an action on a branch and its confirmation: the members it applies to (indented by how far below the
 * root they are) and the maintainers it leaves alone; for a removal also the recommendations choice and the reason.
 */
function BranchPanel({
  member,
  action,
  guard,
  handleStepUpError,
  onDone,
  onCancel,
}: {
  member: TreeMember
  action: BranchAction
  guard: (action: () => void) => void
  handleStepUpError: (error: unknown) => boolean
  onDone: (count: number) => void
  onCancel: () => void
}) {
  const { t } = useTranslation(['members', 'common'])
  const headingId = useId()
  const queryClient = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const previewKey = ['members', 'branch', member.uuid, action] as const
  const preview = useQuery({
    queryKey: previewKey,
    queryFn: () => previewBranch(member.uuid, action),
    staleTime: 0,
    gcTime: 0,
  })
  const execute = useMutation({
    mutationFn: ({ choice, reason }: { choice?: RecommendationsChoice; reason?: string }) => {
      const affected = preview.data?.affected.map((m) => m.uuid) ?? []
      return executeBranch(member.uuid, { action, affected, recommendations: choice, reason })
    },
    onSuccess: (result) => {
      onDone(result.affected.length)
    },
    onError: (err) => {
      if (handleStepUpError(err)) {
        return
      }
      setError(governanceErrorMessage(t, err))
      if (errorCode(err) === 'branch_changed') {
        void preview.refetch()
      }
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: memberTreeQueryKey })
    },
  })
  const confirm = (choice?: RecommendationsChoice, reason?: string) => {
    setError(null)
    guard(() => {
      execute.mutate({ choice, reason })
    })
  }
  const title = t(action === BranchRemove ? 'tree.preview.removeTitle' : 'tree.preview.stopTitle', {
    name: member.displayName,
  })
  const affected = preview.data?.affected ?? []
  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col gap-3 rounded-lg border border-primary/40 bg-muted/40 p-3 text-sm"
    >
      <h3 id={headingId} className="font-medium">
        {title}
      </h3>
      {error && (
        <p role="alert" className="text-destructive">
          {error}
        </p>
      )}
      {preview.isPending && (
        <p role="status" className="text-muted-foreground">
          {t('common:app.loading')}
        </p>
      )}
      {preview.isError && (
        <p role="alert" className="text-destructive">
          {governanceErrorMessage(t, preview.error)}
        </p>
      )}
      {preview.data && (
        <>
          <p>{t('tree.preview.affected', { count: affected.length })}</p>
          {affected.length > 0 && <MemberList label={t('tree.preview.affectedList')} members={affected} />}
          {preview.data.skipped.length > 0 && (
            <>
              <p>{t('tree.preview.skipped', { count: preview.data.skipped.length })}</p>
              <MemberList label={t('tree.preview.skippedList')} members={preview.data.skipped} />
            </>
          )}
          {action === BranchRemove ? (
            <>
              <p className="text-muted-foreground">{t('remove.explanation')}</p>
              <RemovalForm
                confirmLabel={t('tree.preview.confirmRemove', { count: affected.length })}
                pending={execute.isPending || preview.isFetching}
                onConfirm={confirm}
                onCancel={onCancel}
              />
            </>
          ) : (
            <div className="flex flex-wrap gap-2">
              <Button
                disabled={execute.isPending || preview.isFetching}
                onClick={() => {
                  confirm()
                }}
              >
                {t('tree.preview.confirmStop', { count: affected.length })}
              </Button>
              <Button variant="ghost" onClick={onCancel}>
                {t('cancel')}
              </Button>
            </div>
          )}
        </>
      )}
      {!preview.data && (
        <Button variant="ghost" className="self-start" onClick={onCancel}>
          {t('cancel')}
        </Button>
      )}
    </section>
  )
}

// Indentation by depth below the root, as classes (no inline styles), deeper ones at the last step.
const indents = ['', 'pl-4', 'pl-8', 'pl-12', 'pl-16', 'pl-20', 'pl-24']

function MemberList({ label, members }: { label: string; members: BranchMember[] }) {
  const { t } = useTranslation('members')
  return (
    <ul aria-label={label} className="flex flex-col gap-1">
      {members.map((m) => (
        <li key={m.uuid} className={indents[Math.min(m.depth, indents.length - 1)]}>
          {m.displayName}
          {m.role === RoleMaintainer && ` · ${t('maintainer')}`}
        </li>
      ))}
    </ul>
  )
}
