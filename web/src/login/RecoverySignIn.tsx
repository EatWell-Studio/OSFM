import { useMutation } from '@tanstack/react-query'
import { type SyntheticEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import { type Enrolled, errorCode, parseEnrolled, recover, recoveryEnrolTarget } from '@/credentials/api'
import { EnrolCredential } from '@/credentials/EnrolCredential'
import { credentialErrorMessage } from '@/credentials/messages'
import { normalizeLoginName } from '@/lib/passwordTotp'
import { RecoveryCodes } from '@/register/RecoveryCodes'

type Step =
  { kind: 'code'; notice: string | null } | { kind: 'enrol'; ticket: string } | { kind: 'codes'; enrolled: Enrolled }

/**
 * Recovery of a member who signs in only with passkeys and has lost them (D5; docs/architecture.md §3.3): login name
 * and a recovery code, then a new credential (password and authenticator app, or a passkey), then the new recovery
 * codes. The recovery code alone opens nothing else; the member is signed in once the new credential exists, and
 * onDone gets the result.
 */
export function RecoverySignIn({ onDone }: { onDone: (enrolled: Enrolled) => void }) {
  const { t } = useTranslation('auth')
  const [step, setStep] = useState<Step>({ kind: 'code', notice: null })
  const [loginName, setLoginName] = useState('')

  if (step.kind === 'codes') {
    return (
      <RecoveryCodes
        codes={step.enrolled.recoveryCodes}
        continueLabel={step.enrolled.otherPasskeys > 0 ? t('credentials.recover.toMe') : undefined}
        onConfirmed={() => {
          onDone(step.enrolled)
        }}
      />
    )
  }
  if (step.kind === 'enrol') {
    return (
      <div className="flex flex-col gap-4">
        <p role="status">{t('credentials.recover.enrol')}</p>
        <EnrolCredential
          target={recoveryEnrolTarget(step.ticket)}
          loginName={normalizeLoginName(loginName)}
          onEnrolled={(body) => {
            setStep({ kind: 'codes', enrolled: parseEnrolled(body) })
          }}
          onError={(err) => {
            if (errorCode(err) !== 'recovery_expired') {
              return false
            }
            setStep({ kind: 'code', notice: t('credentials.recover.errors.expired') })
            return true
          }}
        />
      </div>
    )
  }
  return (
    <CodeForm
      loginName={loginName}
      onLoginNameChange={setLoginName}
      notice={step.notice}
      onTicket={(ticket) => {
        setStep({ kind: 'enrol', ticket })
      }}
    />
  )
}

function CodeForm({
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
  const { t } = useTranslation('auth')
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)
  const start = useMutation({
    mutationFn: () => recover(normalizeLoginName(loginName), code.trim()),
    onSuccess: onTicket,
    onError: (err) => {
      setError(credentialErrorMessage(err))
    },
  })
  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (loginName.trim() === '' || code.trim() === '') {
      setError(t('login.password.errors.missing'))
      return
    }
    setError(null)
    start.mutate()
  }
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      <p>{t('credentials.recover.explanation')}</p>
      {notice && (
        <p role="status" className="rounded-lg border p-3 text-sm">
          {notice}
        </p>
      )}
      <Field
        label={t('login.password.loginName')}
        name="username"
        autoComplete="username"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        value={loginName}
        onChange={(e) => {
          onLoginNameChange(e.target.value)
        }}
      />
      <Field
        label={t('login.password.recovery')}
        hint={t('login.password.recoveryHint')}
        name="recovery-code"
        autoComplete="off"
        autoCapitalize="characters"
        autoCorrect="off"
        spellCheck={false}
        maxLength={32}
        value={code}
        onChange={(e) => {
          setCode(e.target.value)
        }}
      />
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button type="submit" size="lg" disabled={start.isPending} className="self-start">
        {start.isPending ? t('login.password.working') : t('login.password.next')}
      </Button>
    </form>
  )
}
