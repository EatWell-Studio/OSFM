import { useMutation } from '@tanstack/react-query'
import { type SyntheticEvent, useId, useState } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import type { Entitlement } from '@/lib/entitlement'
import { authErrorCode, authRetryAfter, rateLimitedCode } from '@/lib/authApi'
import { rateLimitMessage } from '@/lib/rateLimit'
import type { Me } from '@/session/session'
import {
  confirmPasswordRegistration,
  isValidLoginName,
  maxPasswordLength,
  minPasswordLength,
  normalizeLoginName,
  normalizeTotpCode,
  type PasswordEnrolment,
  passwordLength,
  type Registered,
  startPasswordRegistration,
} from '@/lib/passwordTotp'
import { RecoveryCodes } from './RecoveryCodes'
import { TotpSecret } from './TotpSecret'

type Step =
  { kind: 'form' } | { kind: 'totp'; enrolment: PasswordEnrolment } | { kind: 'codes'; registered: Registered }

type FieldName = 'loginName' | 'displayName' | 'password' | 'repeat' | 'form'

type Errors = Partial<Record<FieldName, string>>
type T = TFunction<['auth', 'setup']>

/** The field a start error belongs to, with its message. */
function startError(t: T, error: unknown): Errors {
  switch (authErrorCode(error)) {
    case rateLimitedCode:
      return { form: rateLimitMessage(authRetryAfter(error)) }
    case 'invite_invalid':
      return { form: t('register.invalidInvite') }
    case 'setup_code_invalid':
      return { form: t('setup:errors.codeInvalid') }
    case 'setup_done':
      return { form: t('setup:errors.done') }
    case 'instance_name_invalid':
      return { form: t('setup:name.required') }
    case 'login_name_invalid':
      return { loginName: t('register.password.errors.loginNameInvalid') }
    case 'login_name_taken':
      return { loginName: t('register.password.errors.loginNameTaken') }
    case 'display_name_invalid':
      return { displayName: t('register.password.errors.displayNameInvalid') }
    case 'password_too_short':
      return { password: t('register.password.errors.passwordTooShort') }
    case 'password_too_long':
      return { password: t('register.password.errors.passwordTooLong') }
    case 'password_is_login_name':
      return { password: t('register.password.errors.passwordIsLoginName') }
    default:
      return { form: t('register.password.errors.network') }
  }
}

/**
 * Registration with password and TOTP (D3): login name, display name and password; then the authenticator app (QR
 * code, or the secret to type in, or a link that opens the app on this device) and one code from it; then the recovery
 * codes. The member exists, and is signed in, once the code is confirmed; onRegistered runs after the member has
 * confirmed saving the recovery codes.
 */
export function PasswordTotpRegistration({
  entitlement,
  onRegistered,
  continueLabel,
}: {
  entitlement: Entitlement
  onRegistered: (me: Me) => void
  /** The label of the button after the recovery codes, when it does not lead to the map. */
  continueLabel?: string
}) {
  const [step, setStep] = useState<Step>({ kind: 'form' })
  const [notice, setNotice] = useState<string | null>(null)
  // What was typed survives going back to the form (after the enrolment expired, say); the password does not.
  const [names, setNames] = useState({ loginName: '', displayName: '' })

  if (step.kind === 'codes') {
    return (
      <RecoveryCodes
        codes={step.registered.recoveryCodes}
        continueLabel={continueLabel}
        onConfirmed={() => {
          onRegistered(step.registered.me)
        }}
      />
    )
  }
  if (step.kind === 'totp') {
    return (
      <TotpStep
        entitlement={entitlement}
        enrolment={step.enrolment}
        onRegistered={(registered) => {
          setStep({ kind: 'codes', registered })
        }}
        onRestart={(message) => {
          setNotice(message)
          setStep({ kind: 'form' })
        }}
      />
    )
  }
  return (
    <AccountForm
      entitlement={entitlement}
      notice={notice}
      names={names}
      onNamesChange={setNames}
      onStarted={(enrolment) => {
        setNotice(null)
        setStep({ kind: 'totp', enrolment })
      }}
    />
  )
}

function AccountForm({
  entitlement,
  notice,
  names,
  onNamesChange,
  onStarted,
}: {
  entitlement: Entitlement
  notice: string | null
  names: { loginName: string; displayName: string }
  onNamesChange: (names: { loginName: string; displayName: string }) => void
  onStarted: (enrolment: PasswordEnrolment) => void
}) {
  const { t } = useTranslation(['auth', 'setup'])
  const { loginName, displayName } = names
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [errors, setErrors] = useState<Errors>({})

  const start = useMutation({
    mutationFn: (req: { loginName: string; displayName: string; password: string }) =>
      startPasswordRegistration(entitlement, req),
    onSuccess: onStarted,
    onError: (error) => {
      setErrors(startError(t, error))
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const login = normalizeLoginName(loginName)
    const next: Errors = {}
    if (!isValidLoginName(login)) {
      next.loginName = t('register.password.errors.loginNameInvalid')
    }
    const length = passwordLength(password)
    if (length < minPasswordLength) {
      next.password = t('register.password.errors.passwordTooShort')
    } else if (length > maxPasswordLength) {
      next.password = t('register.password.errors.passwordTooLong')
    } else if (normalizeLoginName(password) === login) {
      next.password = t('register.password.errors.passwordIsLoginName')
    } else if (password !== repeat) {
      next.repeat = t('register.password.errors.passwordsDiffer')
    }
    setErrors(next)
    if (Object.keys(next).length > 0) {
      return
    }
    start.mutate({ loginName: login, displayName: displayName.trim(), password })
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      {notice && (
        <p role="status" className="rounded-lg border p-3 text-sm">
          {notice}
        </p>
      )}
      <Field
        label={t('register.password.loginName')}
        hint={t('register.password.loginNameHint')}
        error={errors.loginName}
        name="username"
        autoComplete="username"
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        required
        maxLength={64}
        value={loginName}
        onChange={(e) => {
          onNamesChange({ loginName: e.target.value, displayName })
        }}
      />
      <Field
        label={t('register.password.displayName')}
        hint={t('register.password.displayNameHint')}
        error={errors.displayName}
        name="nickname"
        autoComplete="nickname"
        maxLength={64}
        value={displayName}
        onChange={(e) => {
          onNamesChange({ loginName, displayName: e.target.value })
        }}
      />
      <Field
        label={t('register.password.password')}
        hint={t('register.password.passwordHint', { min: minPasswordLength })}
        error={errors.password}
        type="password"
        name="new-password"
        autoComplete="new-password"
        required
        value={password}
        onChange={(e) => {
          setPassword(e.target.value)
        }}
      />
      <Field
        label={t('register.password.repeat')}
        error={errors.repeat}
        type="password"
        name="repeat-password"
        autoComplete="new-password"
        required
        value={repeat}
        onChange={(e) => {
          setRepeat(e.target.value)
        }}
      />
      {errors.form && (
        <p role="alert" className="text-sm text-destructive">
          {errors.form}
        </p>
      )}
      <Button type="submit" size="lg" disabled={start.isPending} className="self-start">
        {start.isPending ? t('register.password.working') : t('register.password.next')}
      </Button>
    </form>
  )
}

function TotpStep({
  entitlement,
  enrolment,
  onRegistered,
  onRestart,
}: {
  entitlement: Entitlement
  enrolment: PasswordEnrolment
  onRegistered: (registered: Registered) => void
  onRestart: (message: string) => void
}) {
  const { t } = useTranslation(['auth', 'setup'])
  const headingId = useId()
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)

  const confirm = useMutation({
    mutationFn: (req: { enrolment: string; code: string }) => confirmPasswordRegistration(entitlement, req),
    onSuccess: onRegistered,
    onError: (err) => {
      switch (authErrorCode(err)) {
        case 'code_invalid':
          setError(t('register.password.errors.codeInvalid'))
          break
        case 'enrolment_expired':
          onRestart(t('register.password.errors.enrolmentExpired'))
          break
        case 'login_name_taken':
          onRestart(t('register.password.errors.loginNameTaken'))
          break
        case 'invite_invalid':
          setError(t('register.invalidInvite'))
          break
        case 'setup_code_invalid':
          setError(t('setup:errors.codeInvalid'))
          break
        case 'setup_done':
          setError(t('setup:errors.done'))
          break
        case rateLimitedCode:
          setError(rateLimitMessage(authRetryAfter(err)))
          break
        default:
          setError(t('register.password.errors.network'))
      }
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const normalized = normalizeTotpCode(code)
    if (normalized === null) {
      setError(t('register.password.errors.codeForm'))
      return
    }
    setError(null)
    confirm.mutate({ enrolment: enrolment.enrolment, code: normalized })
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('register.password.totpTitle')}
      </h2>
      <p>{t('register.password.totpExplanation')}</p>
      <TotpSecret otpauthUri={enrolment.otpauthUri} secret={enrolment.secret} />
      <form onSubmit={submit} noValidate className="flex flex-col gap-4">
        <Field
          label={t('register.password.code')}
          hint={t('register.password.codeHint')}
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
        <Button type="submit" size="lg" disabled={confirm.isPending} className="self-start">
          {confirm.isPending ? t('register.password.working') : t('register.password.confirm')}
        </Button>
      </form>
    </section>
  )
}
