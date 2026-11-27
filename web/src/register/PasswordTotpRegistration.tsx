import { useMutation } from '@tanstack/react-query'
import { Check, Copy } from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import { authErrorCode, authRetryAfter, rateLimitedCode } from '@/lib/authApi'
import { rateLimitMessage } from '@/lib/rateLimit'
import type { Me } from '@/session/session'
import {
  confirmPasswordRegistration,
  groupSecret,
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

type Step =
  { kind: 'form' } | { kind: 'totp'; enrolment: PasswordEnrolment } | { kind: 'codes'; registered: Registered }

type FieldName = 'loginName' | 'displayName' | 'password' | 'repeat' | 'form'

type Errors = Partial<Record<FieldName, string>>
type T = ReturnType<typeof useTranslation>['t']

/** The field a start error belongs to, with its message. */
function startError(t: T, error: unknown): Errors {
  switch (authErrorCode(error)) {
    case rateLimitedCode:
      return { form: rateLimitMessage(t, authRetryAfter(error)) }
    case 'invite_invalid':
      return { form: t('pages.register.invalidInvite') }
    case 'login_name_invalid':
      return { loginName: t('pages.register.password.errors.loginNameInvalid') }
    case 'login_name_taken':
      return { loginName: t('pages.register.password.errors.loginNameTaken') }
    case 'display_name_invalid':
      return { displayName: t('pages.register.password.errors.displayNameInvalid') }
    case 'password_too_short':
      return { password: t('pages.register.password.errors.passwordTooShort') }
    case 'password_too_long':
      return { password: t('pages.register.password.errors.passwordTooLong') }
    case 'password_is_login_name':
      return { password: t('pages.register.password.errors.passwordIsLoginName') }
    default:
      return { form: t('pages.register.password.errors.network') }
  }
}

/**
 * Registration with password and TOTP (D3): login name, display name and password; then the authenticator app (QR
 * code, or the secret to type in, or a link that opens the app on this device) and one code from it; then the recovery
 * codes. The member exists, and is signed in, once the code is confirmed; onRegistered runs after the member has
 * confirmed saving the recovery codes.
 */
export function PasswordTotpRegistration({ invite, onRegistered }: { invite: string; onRegistered: (me: Me) => void }) {
  const [step, setStep] = useState<Step>({ kind: 'form' })
  const [notice, setNotice] = useState<string | null>(null)
  // What was typed survives going back to the form (after the enrolment expired, say); the password does not.
  const [names, setNames] = useState({ loginName: '', displayName: '' })

  if (step.kind === 'codes') {
    return (
      <RecoveryCodes
        codes={step.registered.recoveryCodes}
        onConfirmed={() => {
          onRegistered(step.registered.me)
        }}
      />
    )
  }
  if (step.kind === 'totp') {
    return (
      <TotpStep
        invite={invite}
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
      invite={invite}
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
  invite,
  notice,
  names,
  onNamesChange,
  onStarted,
}: {
  invite: string
  notice: string | null
  names: { loginName: string; displayName: string }
  onNamesChange: (names: { loginName: string; displayName: string }) => void
  onStarted: (enrolment: PasswordEnrolment) => void
}) {
  const { t } = useTranslation()
  const { loginName, displayName } = names
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [errors, setErrors] = useState<Errors>({})

  const start = useMutation({
    mutationFn: startPasswordRegistration,
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
      next.loginName = t('pages.register.password.errors.loginNameInvalid')
    }
    const length = passwordLength(password)
    if (length < minPasswordLength) {
      next.password = t('pages.register.password.errors.passwordTooShort')
    } else if (length > maxPasswordLength) {
      next.password = t('pages.register.password.errors.passwordTooLong')
    } else if (normalizeLoginName(password) === login) {
      next.password = t('pages.register.password.errors.passwordIsLoginName')
    } else if (password !== repeat) {
      next.repeat = t('pages.register.password.errors.passwordsDiffer')
    }
    setErrors(next)
    if (Object.keys(next).length > 0) {
      return
    }
    start.mutate({ invite, loginName: login, displayName: displayName.trim(), password })
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      {notice && (
        <p role="status" className="rounded-lg border p-3 text-sm">
          {notice}
        </p>
      )}
      <Field
        label={t('pages.register.password.loginName')}
        hint={t('pages.register.password.loginNameHint')}
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
        label={t('pages.register.password.displayName')}
        hint={t('pages.register.password.displayNameHint')}
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
        label={t('pages.register.password.password')}
        hint={t('pages.register.password.passwordHint', { min: minPasswordLength })}
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
        label={t('pages.register.password.repeat')}
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
        {start.isPending ? t('pages.register.password.working') : t('pages.register.password.next')}
      </Button>
    </form>
  )
}

function TotpStep({
  invite,
  enrolment,
  onRegistered,
  onRestart,
}: {
  invite: string
  enrolment: PasswordEnrolment
  onRegistered: (registered: Registered) => void
  onRestart: (message: string) => void
}) {
  const { t } = useTranslation()
  const headingId = useId()
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const confirm = useMutation({
    mutationFn: confirmPasswordRegistration,
    onSuccess: onRegistered,
    onError: (err) => {
      switch (authErrorCode(err)) {
        case 'code_invalid':
          setError(t('pages.register.password.errors.codeInvalid'))
          break
        case 'enrolment_expired':
          onRestart(t('pages.register.password.errors.enrolmentExpired'))
          break
        case 'login_name_taken':
          onRestart(t('pages.register.password.errors.loginNameTaken'))
          break
        case 'invite_invalid':
          setError(t('pages.register.invalidInvite'))
          break
        case rateLimitedCode:
          setError(rateLimitMessage(t, authRetryAfter(err)))
          break
        default:
          setError(t('pages.register.password.errors.network'))
      }
    },
  })

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const normalized = normalizeTotpCode(code)
    if (normalized === null) {
      setError(t('pages.register.password.errors.codeForm'))
      return
    }
    setError(null)
    confirm.mutate({ invite, enrolment: enrolment.enrolment, code: normalized })
  }

  return (
    <section aria-labelledby={headingId} className="flex flex-col gap-4">
      <h2 id={headingId} className="text-lg font-medium">
        {t('pages.register.password.totpTitle')}
      </h2>
      <p>{t('pages.register.password.totpExplanation')}</p>
      <div className="self-center rounded-lg bg-white p-2">
        <QRCodeSVG value={enrolment.otpauthUri} size={208} level="M" title={t('pages.register.password.qrTitle')} />
      </div>
      <Button asChild variant="outline" className="self-start">
        <a href={enrolment.otpauthUri}>{t('pages.register.password.openApp')}</a>
      </Button>
      <div className="flex flex-col gap-1">
        <p className="text-sm">{t('pages.register.password.secretLabel')}</p>
        <div className="flex flex-wrap items-center gap-2">
          <code className="rounded border bg-muted/40 px-2 py-1 font-mono text-sm break-all select-all">
            {groupSecret(enrolment.secret)}
          </code>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              navigator.clipboard.writeText(enrolment.secret).then(
                () => {
                  setCopied(true)
                },
                () => {
                  setCopied(false)
                },
              )
            }}
          >
            {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
            {copied ? t('pages.register.password.copied') : t('pages.register.password.copySecret')}
          </Button>
        </div>
      </div>
      <form onSubmit={submit} noValidate className="flex flex-col gap-4">
        <Field
          label={t('pages.register.password.code')}
          hint={t('pages.register.password.codeHint')}
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
          {confirm.isPending ? t('pages.register.password.working') : t('pages.register.password.confirm')}
        </Button>
      </form>
    </section>
  )
}
