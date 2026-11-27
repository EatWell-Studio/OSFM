import { useMutation } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { type SyntheticEvent, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import {
  maxPasswordLength,
  minPasswordLength,
  normalizeTotpCode,
  type PasswordEnrolment,
  passwordLength,
} from '@/lib/passwordTotp'
import { passkeysSupported } from '@/lib/webauthn'
import { TotpSecret } from '@/register/TotpSecret'
import { confirmPasswordEnrolment, type EnrolTarget, enrolPasskey, errorCode, startPasswordEnrolment } from './api'
import { credentialErrorMessage } from './messages'

type Method = 'password' | 'passkey'

/**
 * Enrols a new credential for a member who exists: on the "me" page, after a recovery code, or with a reset link. The
 * member chooses the method (password and authenticator app first, then passkey; methods limits the choice), and
 * onEnrolled gets the server's response to the last request. onError sees every failure first and may take it over
 * (the "me" page asks for a re-verification): it returns true when it did.
 */
export function EnrolCredential({
  target,
  loginName,
  methods = ['password', 'passkey'],
  askDeviceName = false,
  onEnrolled,
  onError,
}: {
  target: EnrolTarget
  /** The member's login name, for password managers. */
  loginName?: string
  methods?: Method[]
  askDeviceName?: boolean
  onEnrolled: (body: unknown) => void
  onError?: (error: unknown) => boolean
}) {
  const { t } = useTranslation()
  const [method, setMethod] = useState<Method>(methods[0] ?? 'password')
  return (
    <div className="flex flex-col gap-4">
      {methods.length > 1 && (
        <div role="group" aria-label={t('credentials.enrol.method')} className="flex flex-wrap gap-2">
          {methods.map((m) => (
            <Button
              key={m}
              variant={m === method ? 'default' : 'outline'}
              aria-pressed={m === method}
              onClick={() => {
                setMethod(m)
              }}
            >
              {m === 'password' ? t('pages.register.methods.password') : t('pages.register.methods.passkey')}
            </Button>
          ))}
        </div>
      )}
      {method === 'password' ? (
        <PasswordEnrol target={target} loginName={loginName} onEnrolled={onEnrolled} onError={onError} />
      ) : (
        <PasskeyEnrol target={target} askDeviceName={askDeviceName} onEnrolled={onEnrolled} onError={onError} />
      )}
    </div>
  )
}

function PasswordEnrol({
  target,
  loginName,
  onEnrolled,
  onError,
}: {
  target: EnrolTarget
  loginName?: string
  onEnrolled: (body: unknown) => void
  onError?: (error: unknown) => boolean
}) {
  const { t } = useTranslation()
  const [password, setPassword] = useState('')
  const [repeat, setRepeat] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [enrolment, setEnrolment] = useState<PasswordEnrolment | null>(null)

  const start = useMutation({
    mutationFn: () => startPasswordEnrolment(target, password),
    onSuccess: (e) => {
      setEnrolment(e)
      setError(null)
    },
    onError: (err) => {
      if (!onError?.(err)) {
        setError(credentialErrorMessage(t, err))
      }
    },
  })

  if (enrolment) {
    return (
      <TotpConfirm
        enrolment={enrolment}
        confirm={(code) => confirmPasswordEnrolment(target, enrolment.enrolment, code)}
        onConfirmed={onEnrolled}
        onRestart={(message) => {
          setEnrolment(null)
          setError(message)
        }}
        onError={onError}
      />
    )
  }

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const length = passwordLength(password)
    if (length < minPasswordLength) {
      setError(t('pages.register.password.errors.passwordTooShort'))
    } else if (length > maxPasswordLength) {
      setError(t('pages.register.password.errors.passwordTooLong'))
    } else if (password !== repeat) {
      setError(t('pages.register.password.errors.passwordsDiffer'))
    } else {
      setError(null)
      start.mutate()
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-4">
      {/* For password managers: the account the new password belongs to. */}
      {loginName && <input type="text" name="username" autoComplete="username" value={loginName} readOnly hidden />}
      <Field
        label={t('credentials.enrol.password')}
        hint={t('pages.register.password.passwordHint', { min: minPasswordLength })}
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
        type="password"
        name="repeat-password"
        autoComplete="new-password"
        required
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
      <Button type="submit" size="lg" disabled={start.isPending} className="self-start">
        {start.isPending ? t('pages.register.password.working') : t('pages.register.password.next')}
      </Button>
    </form>
  )
}

/**
 * The authenticator app step of a set-up: the new secret, and one code from the app to confirm it. confirm sends the
 * code; onRestart is called when the pending set-up has expired.
 */
export function TotpConfirm<R>({
  enrolment,
  confirm,
  onConfirmed,
  onRestart,
  onError,
}: {
  enrolment: PasswordEnrolment
  confirm: (code: string) => Promise<R>
  onConfirmed: (result: R) => void
  onRestart: (message: string) => void
  onError?: (error: unknown) => boolean
}) {
  const { t } = useTranslation()
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)
  const verify = useMutation({
    mutationFn: confirm,
    onSuccess: onConfirmed,
    onError: (err) => {
      if (onError?.(err)) {
        return
      }
      if (errorCode(err) === 'enrolment_expired') {
        onRestart(t('credentials.errors.expired'))
      } else {
        setError(credentialErrorMessage(t, err))
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
    verify.mutate(normalized)
  }
  return (
    <div className="flex flex-col gap-4">
      <p>{t('pages.register.password.totpExplanation')}</p>
      <TotpSecret otpauthUri={enrolment.otpauthUri} secret={enrolment.secret} />
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
        <Button type="submit" size="lg" disabled={verify.isPending} className="self-start">
          {verify.isPending ? t('pages.register.password.working') : t('pages.register.password.confirm')}
        </Button>
      </form>
    </div>
  )
}

function PasskeyEnrol({
  target,
  askDeviceName,
  onEnrolled,
  onError,
}: {
  target: EnrolTarget
  askDeviceName: boolean
  onEnrolled: (body: unknown) => void
  onError?: (error: unknown) => boolean
}) {
  const { t } = useTranslation()
  const [deviceName, setDeviceName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: () => enrolPasskey(target, deviceName.trim()),
    onSuccess: onEnrolled,
    onError: (err) => {
      if (!onError?.(err)) {
        setError(credentialErrorMessage(t, err))
      }
    },
  })
  if (!passkeysSupported()) {
    return (
      <p role="alert" className="text-muted-foreground">
        {t('passkey.errors.unsupported')}
      </p>
    )
  }
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        setError(null)
        create.mutate()
      }}
    >
      {askDeviceName && (
        <Field
          label={t('credentials.passkeys.deviceName')}
          hint={t('credentials.passkeys.deviceNameHint')}
          name="device-name"
          autoComplete="off"
          maxLength={64}
          value={deviceName}
          onChange={(e) => {
            setDeviceName(e.target.value)
          }}
        />
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button type="submit" size="lg" disabled={create.isPending} className="self-start">
        <KeyRound aria-hidden="true" />
        {create.isPending ? t('passkey.register.busy') : t('passkey.register.submit')}
      </Button>
    </form>
  )
}
