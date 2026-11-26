import { useMutation } from '@tanstack/react-query'
import { type SyntheticEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import { authErrorCode } from '@/lib/authApi'
import { normalizeTotpCode, type SignedIn, signInWithCode, signInWithPassword } from '@/lib/passwordTotp'
import type { Me } from '@/session/session'

type Step =
  | { kind: 'password'; notice: string | null }
  | { kind: 'second'; ticket: string }
  | { kind: 'recovered'; signedIn: SignedIn }

/**
 * Sign-in with password (D4): login name and password, then a code from the authenticator app or a recovery code.
 * The server starts the session only after the second step; onSignedIn then gets the member.
 */
export function PasswordSignIn({ onSignedIn }: { onSignedIn: (me: Me) => void }) {
  const { t } = useTranslation()
  const [step, setStep] = useState<Step>({ kind: 'password', notice: null })
  const [loginName, setLoginName] = useState('')

  if (step.kind === 'recovered') {
    return (
      <div className="flex flex-col gap-4">
        <p role="status">
          {t('pages.login.password.recoveryCodesLeft', { count: step.signedIn.recoveryCodesLeft ?? 0 })}
        </p>
        <Button
          size="lg"
          className="self-start"
          onClick={() => {
            onSignedIn(step.signedIn.me)
          }}
        >
          {t('pages.login.password.continue')}
        </Button>
      </div>
    )
  }
  if (step.kind === 'second') {
    return (
      <SecondFactor
        ticket={step.ticket}
        onSignedIn={(signedIn) => {
          if (signedIn.recoveryCodesLeft === undefined) {
            onSignedIn(signedIn.me)
          } else {
            setStep({ kind: 'recovered', signedIn })
          }
        }}
        onRestart={(notice) => {
          setStep({ kind: 'password', notice })
        }}
      />
    )
  }
  return (
    <PasswordStep
      loginName={loginName}
      onLoginNameChange={setLoginName}
      notice={step.notice}
      onTicket={(ticket) => {
        setStep({ kind: 'second', ticket })
      }}
    />
  )
}

function PasswordStep({
  loginName,
  onLoginNameChange,
  notice,
  onTicket,
}: {
  loginName: string
  onLoginNameChange: (s: string) => void
  notice: string | null
  onTicket: (ticket: string) => void
}) {
  const { t } = useTranslation()
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)

  const signIn = useMutation({
    mutationFn: () => signInWithPassword(loginName.trim().toLowerCase(), password),
    onSuccess: onTicket,
    onError: (err) => {
      setPassword('')
      setError(
        authErrorCode(err) === 'credentials_invalid'
          ? t('pages.login.password.errors.credentials')
          : t('pages.login.password.errors.network'),
      )
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (loginName.trim() === '' || password === '') {
      setError(t('pages.login.password.errors.missing'))
      return
    }
    setError(null)
    signIn.mutate()
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      {notice && (
        <p role="status" className="rounded-lg border p-3 text-sm">
          {notice}
        </p>
      )}
      <Field
        label={t('pages.login.password.loginName')}
        name="username"
        autoComplete="username"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        required
        value={loginName}
        onChange={(e) => {
          onLoginNameChange(e.target.value)
        }}
      />
      <Field
        label={t('pages.login.password.password')}
        type="password"
        name="password"
        autoComplete="current-password"
        required
        value={password}
        onChange={(e) => {
          setPassword(e.target.value)
        }}
      />
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button type="submit" size="lg" disabled={signIn.isPending} className="self-start">
        {signIn.isPending ? t('pages.login.password.working') : t('pages.login.password.next')}
      </Button>
    </form>
  )
}

function SecondFactor({
  ticket,
  onSignedIn,
  onRestart,
}: {
  ticket: string
  onSignedIn: (signedIn: SignedIn) => void
  onRestart: (notice: string) => void
}) {
  const { t } = useTranslation()
  const [kind, setKind] = useState<'totp' | 'recovery'>('totp')
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)

  const verify = useMutation({
    mutationFn: (c: string) => signInWithCode(kind, ticket, c),
    onSuccess: onSignedIn,
    onError: (err) => {
      switch (authErrorCode(err)) {
        case 'code_invalid':
          setError(
            kind === 'totp'
              ? t('pages.login.password.errors.totpInvalid')
              : t('pages.login.password.errors.recoveryInvalid'),
          )
          break
        case 'ticket_expired':
          onRestart(t('pages.login.password.errors.expired'))
          break
        case 'cannot_sign_in':
          onRestart(t('pages.login.password.errors.cannotSignIn'))
          break
        default:
          setError(t('pages.login.password.errors.network'))
      }
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    let c: string | null = code.trim()
    if (kind === 'totp') {
      c = normalizeTotpCode(code)
      if (c === null) {
        setError(t('pages.login.password.errors.totpForm'))
        return
      }
    } else if (c === '') {
      setError(t('pages.login.password.errors.missing'))
      return
    }
    setError(null)
    verify.mutate(c)
  }

  const switchKind = () => {
    setKind(kind === 'totp' ? 'recovery' : 'totp')
    setCode('')
    setError(null)
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <p>
        {kind === 'totp' ? t('pages.login.password.totpExplanation') : t('pages.login.password.recoveryExplanation')}
      </p>
      {kind === 'totp' ? (
        <Field
          key="totp"
          label={t('pages.login.password.totp')}
          error={error}
          name="one-time-code"
          autoComplete="one-time-code"
          inputMode="numeric"
          maxLength={7}
          required
          value={code}
          onChange={(e) => {
            setCode(e.target.value)
          }}
        />
      ) : (
        <Field
          key="recovery"
          label={t('pages.login.password.recovery')}
          hint={t('pages.login.password.recoveryHint')}
          error={error}
          name="recovery-code"
          autoComplete="off"
          autoCapitalize="characters"
          autoCorrect="off"
          spellCheck={false}
          maxLength={32}
          required
          value={code}
          onChange={(e) => {
            setCode(e.target.value)
          }}
        />
      )}
      <Button type="submit" size="lg" disabled={verify.isPending} className="self-start">
        {verify.isPending ? t('pages.login.password.working') : t('pages.login.password.signIn')}
      </Button>
      <Button type="button" variant="link" onClick={switchKind} className="self-start px-0">
        {kind === 'totp' ? t('pages.login.password.useRecovery') : t('pages.login.password.useTotp')}
      </Button>
    </form>
  )
}
