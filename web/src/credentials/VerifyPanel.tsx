import { useMutation } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { type SyntheticEvent, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Field } from '@/components/Field'
import { Button } from '@/components/ui/button'
import { passkeysSupported } from '@/lib/webauthn'
import { type Credentials, verifyWithPasskey, verifyWithPassword } from './api'
import { credentialErrorMessage } from './messages'

/** Asks the member to confirm it is them: with the password and a code (first, when they have one) or a passkey. */
export function VerifyPanel({
  credentials,
  repeat,
  onVerified,
  onCancel,
}: {
  credentials: Credentials
  /** Whether the member has to repeat the step they were taking afterwards. */
  repeat: boolean
  onVerified: (verifiedUntil: string) => void
  onCancel: () => void
}) {
  const { t } = useTranslation('auth')
  const headingId = useId()
  const methods: ('password' | 'passkey')[] = []
  if (credentials.passwordTotp) {
    methods.push('password')
  }
  if (credentials.passkeys.length > 0 && passkeysSupported()) {
    methods.push('passkey')
  }
  const [method, setMethod] = useState(methods[0] ?? 'password')
  return (
    <section
      aria-labelledby={headingId}
      className="flex flex-col gap-3 rounded-lg border border-primary/40 bg-muted/40 p-3"
    >
      <h3 id={headingId} className="font-medium">
        {t('credentials.verify.title')}
      </h3>
      <p className="text-sm">{repeat ? t('credentials.verify.repeat') : t('credentials.verify.explanation')}</p>
      {methods.length > 1 && (
        <div role="group" aria-label={t('credentials.verify.method')} className="flex flex-wrap gap-2">
          {methods.map((m) => (
            <Button
              key={m}
              size="sm"
              variant={m === method ? 'default' : 'outline'}
              aria-pressed={m === method}
              onClick={() => {
                setMethod(m)
              }}
            >
              {m === 'password' ? t('credentials.verify.withPassword') : t('credentials.verify.withPasskey')}
            </Button>
          ))}
        </div>
      )}
      {method === 'password' ? <PasswordVerify onVerified={onVerified} /> : <PasskeyVerify onVerified={onVerified} />}
      <Button variant="ghost" size="sm" className="self-start" onClick={onCancel}>
        {t('credentials.cancel')}
      </Button>
    </section>
  )
}

function PasswordVerify({ onVerified }: { onVerified: (until: string) => void }) {
  const { t } = useTranslation('auth')
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState<string | null>(null)
  const verify = useMutation({
    mutationFn: () => verifyWithPassword(password, code.trim()),
    onSuccess: onVerified,
    onError: (err) => {
      setError(credentialErrorMessage(err))
    },
  })
  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    if (password === '' || code.trim() === '') {
      setError(t('login.password.errors.missing'))
      return
    }
    setError(null)
    verify.mutate()
  }
  return (
    <form onSubmit={submit} noValidate className="flex flex-col gap-3">
      <Field
        label={t('login.password.password')}
        type="password"
        name="password"
        autoComplete="current-password"
        value={password}
        onChange={(e) => {
          setPassword(e.target.value)
        }}
      />
      <Field
        label={t('credentials.verify.code')}
        hint={t('credentials.verify.codeHint')}
        name="one-time-code"
        autoComplete="one-time-code"
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
      <Button type="submit" disabled={verify.isPending} className="self-start">
        {verify.isPending ? t('register.password.working') : t('credentials.verify.submit')}
      </Button>
    </form>
  )
}

function PasskeyVerify({ onVerified }: { onVerified: (until: string) => void }) {
  const { t } = useTranslation('auth')
  const [error, setError] = useState<string | null>(null)
  const verify = useMutation({
    mutationFn: verifyWithPasskey,
    onSuccess: onVerified,
    onError: (err) => {
      setError(credentialErrorMessage(err))
    },
  })
  return (
    <div className="flex flex-col gap-3">
      <Button
        disabled={verify.isPending}
        className="self-start"
        onClick={() => {
          setError(null)
          verify.mutate()
        }}
      >
        <KeyRound aria-hidden="true" />
        {verify.isPending ? t('passkey.signIn.busy') : t('credentials.verify.passkeyButton')}
      </Button>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
    </div>
  )
}
