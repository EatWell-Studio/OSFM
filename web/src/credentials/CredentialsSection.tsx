import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, Plus } from 'lucide-react'
import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import { type PasswordEnrolment, maxPasswordLength, minPasswordLength, passwordLength } from '@/lib/passwordTotp'
import { RecoveryCodes } from '@/register/RecoveryCodes'
import {
  changePassword,
  confirmReplaceTotp,
  type Credentials,
  credentialsQueryKey,
  fetchCredentials,
  meEnrolTarget,
  parseCredentials,
  type PasskeyInfo,
  regenerateRecoveryCodes,
  removePasskey,
  removePassword,
  renamePasskey,
  startReplaceTotp,
} from './api'
import { EnrolCredential, TotpConfirm } from './EnrolCredential'
import { credentialErrorMessage } from './messages'
import { useStepUp } from './useStepUp'

type Mode =
  | { kind: 'none' }
  | { kind: 'add-password' }
  | { kind: 'add-passkey' }
  | { kind: 'change-password' }
  | { kind: 'replace-totp'; enrolment: PasswordEnrolment | null }
  | { kind: 'codes'; codes: string[] }

/**
 * The member's credentials on the "me" page (D5): password and authenticator app first, then the passkeys, then the
 * recovery codes. Removing, adding and changing need a recent re-verification (useStepUp); the last way to sign in
 * cannot be removed (the server refuses it too).
 */
export function CredentialsSection({ loginName }: { loginName?: string }) {
  const { t } = useTranslation(['auth', 'common'])
  const queryClient = useQueryClient()
  const headingId = useId()
  const query = useQuery({ queryKey: credentialsQueryKey, queryFn: fetchCredentials })
  const credentials = query.data
  const stepUp = useStepUp(credentials)
  const [mode, setMode] = useState<Mode>({ kind: 'none' })
  const [notice, setNotice] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const updated = (c: Credentials, message?: string) => {
    queryClient.setQueryData(credentialsQueryKey, c)
    setMode({ kind: 'none' })
    setError(null)
    setNotice(message ?? null)
  }
  const failed = (err: unknown) => {
    if (!stepUp.handleError(err)) {
      setError(credentialErrorMessage(err))
    }
  }
  const open = (next: Mode) => {
    setNotice(null)
    setError(null)
    stepUp.guard(() => {
      setMode(next)
    })
  }

  const remove = useMutation({
    mutationFn: (uuid: string | null) => (uuid === null ? removePassword() : removePasskey(uuid)),
    onSuccess: (c) => {
      updated(c, t('credentials.removed'))
    },
    onError: failed,
  })
  const regenerate = useMutation({
    mutationFn: regenerateRecoveryCodes,
    onSuccess: (codes) => {
      setMode({ kind: 'codes', codes })
      void queryClient.invalidateQueries({ queryKey: credentialsQueryKey })
    },
    onError: failed,
  })
  const startTotp = useMutation({
    mutationFn: startReplaceTotp,
    onSuccess: (enrolment) => {
      setMode({ kind: 'replace-totp', enrolment })
    },
    onError: failed,
  })

  if (query.isPending) {
    return (
      <p role="status" className="text-muted-foreground">
        {t('common:app.loading')}
      </p>
    )
  }
  if (!credentials) {
    return (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p>{t('credentials.loadError')}</p>
        <Button
          variant="outline"
          onClick={() => {
            void query.refetch()
          }}
        >
          {t('common:retry')}
        </Button>
      </div>
    )
  }

  if (mode.kind === 'codes') {
    return (
      <RecoveryCodes
        codes={mode.codes}
        continueLabel={t('credentials.done')}
        onConfirmed={() => {
          setMode({ kind: 'none' })
          setNotice(t('credentials.recovery.regenerated'))
        }}
      />
    )
  }

  const canRemovePasskey = credentials.passkeys.length > 1 || credentials.passwordTotp !== null
  const canRemovePassword = credentials.passkeys.length > 0
  const date = (iso: string) => new Intl.DateTimeFormat(undefined, { dateStyle: 'medium' }).format(new Date(iso))
  // The server records only the UTC day of the last use (midnight UTC): shown as that day, in any time zone.
  const day = (iso: string) =>
    new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(iso))
  const cancel = (
    <Button
      variant="ghost"
      size="sm"
      className="self-start"
      onClick={() => {
        setMode({ kind: 'none' })
      }}
    >
      {t('credentials.cancel')}
    </Button>
  )

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('credentials.title')}
      </h2>
      {stepUp.panel}
      {notice && (
        <p role="status" className="rounded-lg border p-3 text-sm">
          {notice}
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}

      <div className="flex flex-col gap-2 rounded-lg border p-3">
        <h3 className="font-medium">{t('register.methods.password')}</h3>
        {credentials.passwordTotp ? (
          <>
            <p className="text-sm text-muted-foreground">
              {t('credentials.password.since', { date: date(credentials.passwordTotp.createdAt) })}
              {credentials.passwordTotp.lastUsedAt &&
                ` · ${t('credentials.lastUsed', { date: day(credentials.passwordTotp.lastUsedAt) })}`}
            </p>
            {mode.kind === 'change-password' ? (
              <>
                <ChangePasswordForm loginName={loginName} onDone={updated} onError={failed} />
                {cancel}
              </>
            ) : mode.kind === 'replace-totp' && mode.enrolment ? (
              <>
                <TotpConfirm
                  enrolment={mode.enrolment}
                  confirm={(code) => confirmReplaceTotp(mode.enrolment?.enrolment ?? '', code)}
                  onConfirmed={(c) => {
                    updated(c, t('credentials.password.totpReplaced'))
                  }}
                  onRestart={(message) => {
                    setMode({ kind: 'none' })
                    setError(message)
                  }}
                  onError={stepUp.handleError}
                />
                {cancel}
              </>
            ) : (
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    open({ kind: 'change-password' })
                  }}
                >
                  {t('credentials.password.change')}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={startTotp.isPending}
                  onClick={() => {
                    setNotice(null)
                    setError(null)
                    stepUp.guard(() => {
                      startTotp.mutate()
                    })
                  }}
                >
                  {t('credentials.password.replaceTotp')}
                </Button>
                <RemoveButton
                  disabled={!canRemovePassword || remove.isPending}
                  onConfirm={() => {
                    stepUp.guard(() => {
                      remove.mutate(null)
                    })
                  }}
                />
              </div>
            )}
            {!canRemovePassword && (
              <p className="text-xs text-muted-foreground">{t('credentials.lastCredentialHint')}</p>
            )}
          </>
        ) : mode.kind === 'add-password' ? (
          <>
            <EnrolCredential
              target={meEnrolTarget}
              loginName={loginName}
              methods={['password']}
              onEnrolled={(body) => {
                updated(parseCredentials(body), t('credentials.added'))
              }}
              onError={stepUp.handleError}
            />
            {cancel}
          </>
        ) : (
          <>
            <p className="text-sm text-muted-foreground">{t('credentials.password.none')}</p>
            <Button
              variant="outline"
              size="sm"
              className="self-start"
              onClick={() => {
                open({ kind: 'add-password' })
              }}
            >
              <Plus aria-hidden="true" />
              {t('credentials.password.add')}
            </Button>
          </>
        )}
      </div>

      <div className="flex flex-col gap-2 rounded-lg border p-3">
        <h3 className="font-medium">{t('credentials.passkeys.title')}</h3>
        {credentials.passkeys.length === 0 && (
          <p className="text-sm text-muted-foreground">{t('credentials.passkeys.none')}</p>
        )}
        {credentials.passkeys.length > 0 && (
          <ul aria-label={t('credentials.passkeys.title')} className="flex flex-col divide-y">
            {credentials.passkeys.map((p) => (
              <PasskeyRow
                key={p.uuid}
                passkey={p}
                date={date}
                day={day}
                canRemove={canRemovePasskey && !remove.isPending}
                onRenamed={(c) => {
                  updated(c)
                }}
                onRemove={() => {
                  setNotice(null)
                  setError(null)
                  stepUp.guard(() => {
                    remove.mutate(p.uuid)
                  })
                }}
                onError={failed}
              />
            ))}
          </ul>
        )}
        {credentials.passkeys.length > 0 && !canRemovePasskey && (
          <p className="text-xs text-muted-foreground">{t('credentials.lastCredentialHint')}</p>
        )}
        {mode.kind === 'add-passkey' ? (
          <>
            <EnrolCredential
              target={meEnrolTarget}
              methods={['passkey']}
              askDeviceName
              onEnrolled={(body) => {
                updated(parseCredentials(body), t('credentials.added'))
              }}
              onError={stepUp.handleError}
            />
            {cancel}
          </>
        ) : (
          <Button
            variant="outline"
            size="sm"
            className="self-start"
            onClick={() => {
              open({ kind: 'add-passkey' })
            }}
          >
            <KeyRound aria-hidden="true" />
            {t('credentials.passkeys.add')}
          </Button>
        )}
      </div>

      <div className="flex flex-col gap-2 rounded-lg border p-3">
        <h3 className="font-medium">{t('credentials.recovery.title')}</h3>
        <p className="text-sm text-muted-foreground">
          {t('credentials.recovery.left', { count: credentials.recoveryCodesLeft })}
        </p>
        <Button
          variant="outline"
          size="sm"
          className="self-start"
          disabled={regenerate.isPending}
          onClick={() => {
            setNotice(null)
            setError(null)
            stepUp.guard(() => {
              regenerate.mutate()
            })
          }}
        >
          {t('credentials.recovery.regenerate')}
        </Button>
        <p className="text-xs text-muted-foreground">{t('credentials.recovery.hint')}</p>
      </div>
    </section>
  )
}

/** A remove button that asks once more before it acts. */
function RemoveButton({ disabled, onConfirm }: { disabled: boolean; onConfirm: () => void }) {
  const { t } = useTranslation(['auth', 'common'])
  const [asking, setAsking] = useState(false)
  if (asking) {
    return (
      <span className="flex flex-wrap items-center gap-2">
        <Button
          variant="destructive"
          size="sm"
          onClick={() => {
            setAsking(false)
            onConfirm()
          }}
        >
          {t('credentials.confirmRemove')}
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setAsking(false)
          }}
        >
          {t('credentials.cancel')}
        </Button>
      </span>
    )
  }
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={disabled}
      onClick={() => {
        setAsking(true)
      }}
    >
      {t('credentials.remove')}
    </Button>
  )
}

function PasskeyRow({
  passkey,
  date,
  day,
  canRemove,
  onRenamed,
  onRemove,
  onError,
}: {
  passkey: PasskeyInfo
  date: (iso: string) => string
  day: (iso: string) => string
  canRemove: boolean
  onRenamed: (c: Credentials) => void
  onRemove: () => void
  onError: (err: unknown) => void
}) {
  const { t } = useTranslation(['auth', 'common'])
  const [renaming, setRenaming] = useState(false)
  const [name, setName] = useState(passkey.deviceName)
  const rename = useMutation({
    mutationFn: () => renamePasskey(passkey.uuid, name.trim()),
    onSuccess: (c) => {
      setRenaming(false)
      onRenamed(c)
    },
    onError,
  })
  return (
    <li className="flex flex-col gap-1 py-2 text-sm">
      <span className="font-medium">{passkey.deviceName || t('credentials.passkeys.unnamed')}</span>
      <span className="text-muted-foreground">
        {t('credentials.passkeys.added', { date: date(passkey.createdAt) })}
        {' · '}
        {passkey.lastUsedAt ? t('credentials.lastUsed', { date: day(passkey.lastUsedAt) }) : t('credentials.neverUsed')}
      </span>
      {renaming ? (
        <form
          className="flex flex-col gap-2"
          onSubmit={(e: SyntheticEvent) => {
            e.preventDefault()
            rename.mutate()
          }}
        >
          <Field
            label={t('credentials.passkeys.deviceName')}
            maxLength={64}
            value={name}
            onChange={(e) => {
              setName(e.target.value)
            }}
          />
          <span className="flex gap-2">
            <Button type="submit" size="sm" disabled={rename.isPending}>
              {t('credentials.save')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => {
                setRenaming(false)
              }}
            >
              {t('credentials.cancel')}
            </Button>
          </span>
        </form>
      ) : (
        <span className="flex flex-wrap gap-2">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setRenaming(true)
            }}
          >
            {t('credentials.passkeys.rename')}
          </Button>
          <RemoveButton disabled={!canRemove} onConfirm={onRemove} />
        </span>
      )}
    </li>
  )
}

function ChangePasswordForm({
  loginName,
  onDone,
  onError,
}: {
  loginName?: string
  onDone: (c: Credentials, message: string) => void
  onError: (err: unknown) => void
}) {
  const { t } = useTranslation(['auth', 'common'])
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [error, setError] = useState<string | null>(null)
  const change = useMutation({
    mutationFn: () => changePassword(password),
    onSuccess: (c) => {
      onDone(c, t('credentials.password.changed'))
    },
    onError,
  })
  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const length = passwordLength(password)
    if (length < minPasswordLength) {
      setError(t('register.password.errors.passwordTooShort'))
    } else if (length > maxPasswordLength) {
      setError(t('register.password.errors.passwordTooLong'))
    } else if (password !== repeat) {
      setError(t('register.password.errors.passwordsDiffer'))
    } else {
      setError(null)
      change.mutate()
    }
  }
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-3">
      {loginName && <input type="text" name="username" autoComplete="username" value={loginName} readOnly hidden />}
      <Field
        label={t('credentials.password.new')}
        hint={t('register.password.passwordHint', { min: minPasswordLength })}
        type="password"
        name="new-password"
        autoComplete="new-password"
        value={password}
        onChange={(e) => {
          setPassword(e.target.value)
        }}
      />
      <Field
        label={t('register.password.repeat')}
        type="password"
        name="repeat-password"
        autoComplete="new-password"
        value={repeat}
        onChange={(e) => {
          setRepeat(e.target.value)
        }}
      />
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button type="submit" size="sm" disabled={change.isPending} className="self-start">
        {t('credentials.password.change')}
      </Button>
    </form>
  )
}
