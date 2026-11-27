import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { type Entitlement, registrationPaths } from '@/lib/entitlement'
import {
  creationOptionsFromJSON,
  PasskeyError,
  passkeyErrorFrom,
  passkeysSupported,
  postJson,
  readCeremonyStart,
  registrationToJSON,
} from '@/lib/webauthn'
import { rateLimitMessage } from '@/lib/rateLimit'
import { RecoveryCodes } from './RecoveryCodes'

/** The error messages of the registration form, by server error code or ceremony failure. */
const messageKeys = {
  login_name_invalid: 'passkey.register.errors.invalidLoginName',
  display_name_invalid: 'passkey.register.errors.invalidDisplayName',
  display_name_is_login_name: 'passkey.register.errors.displayNameIsLoginName',
  name_taken: 'passkey.register.errors.nameTaken',
  login_name_taken: 'passkey.register.errors.loginNameTaken',
  invite_invalid: 'passkey.register.errors.invalidInvite',
  setup_code_invalid: 'setup:errors.codeInvalid',
  setup_done: 'setup:errors.done',
  instance_name_invalid: 'setup:name.required',
  passkey_failed: 'passkey.register.errors.verifyFailed',
  passkeys_unavailable: 'passkey.errors.unavailable',
  cancelled: 'passkey.errors.cancelled',
  exists: 'passkey.register.errors.exists',
  address: 'passkey.errors.address',
  unsupported: 'passkey.errors.unsupported',
  failed: 'passkey.errors.failed',
} as const

function messageKey(e: PasskeyError) {
  if (e.code !== undefined && e.code in messageKeys) {
    return messageKeys[e.code as keyof typeof messageKeys]
  }
  return messageKeys[e.reason]
}

/**
 * Registration with a passkey (D3): login name and display name, then the browser creates a passkey, the server
 * creates the member with it and signs them in; the recovery codes are shown once and must be confirmed before
 * onDone is called. entitlement is the invite (or, for the first maintainer, the setup code).
 */
export function PasskeyRegistration({
  entitlement,
  onDone,
  continueLabel,
}: {
  entitlement: Entitlement
  onDone: () => void
  /** The label of the button after the recovery codes, when it does not lead to the map. */
  continueLabel?: string
}) {
  const { t } = useTranslation(['auth', 'setup'])
  const id = useId()
  const [loginName, setLoginName] = useState('')
  const [displayName, setDisplayName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<PasskeyError | null>(null)
  const [codes, setCodes] = useState<string[] | null>(null)

  if (codes !== null) {
    return <RecoveryCodes codes={codes} onConfirmed={onDone} continueLabel={continueLabel} />
  }
  if (!passkeysSupported()) {
    return (
      <p role="alert" className="text-muted-foreground">
        {t('passkey.errors.unsupported')}
      </p>
    )
  }

  async function register() {
    const paths = registrationPaths(entitlement)
    const start = await readCeremonyStart(
      await postJson(paths.passkeyOptions, { ...entitlement, loginName, displayName }),
    )
    let credential: Credential | null
    try {
      credential = await navigator.credentials.create(creationOptionsFromJSON(start.options))
    } catch (e) {
      throw passkeyErrorFrom(e)
    }
    if (!(credential instanceof PublicKeyCredential)) {
      throw new PasskeyError('failed')
    }
    const response = await postJson(paths.passkey, {
      ...entitlement,
      state: start.state,
      credential: registrationToJSON(credential),
    })
    const body: unknown = await response.json()
    const list = typeof body === 'object' && body !== null ? (body as Record<string, unknown>).recoveryCodes : null
    if (!Array.isArray(list) || !list.every((c) => typeof c === 'string')) {
      throw new PasskeyError('failed')
    }
    setCodes(list)
  }

  const fieldError =
    error?.code === 'login_name_invalid' || error?.code === 'login_name_taken'
      ? 'login'
      : error?.code === 'display_name_invalid' ||
          error?.code === 'display_name_is_login_name' ||
          error?.code === 'name_taken'
        ? 'display'
        : null

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(e) => {
        e.preventDefault()
        setBusy(true)
        setError(null)
        register()
          .catch((err: unknown) => {
            setError(passkeyErrorFrom(err))
          })
          .finally(() => {
            setBusy(false)
          })
      }}
    >
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-login`} className="text-sm font-medium">
          {t('passkey.register.loginName')}
        </label>
        <input
          id={`${id}-login`}
          name="username"
          required
          minLength={3}
          maxLength={32}
          pattern="[A-Za-z0-9_\-]+"
          autoComplete="username"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          value={loginName}
          aria-invalid={fieldError === 'login'}
          aria-describedby={`${id}-login-hint`}
          onChange={(e) => {
            setLoginName(e.target.value)
          }}
          className="h-10 rounded-lg border bg-background px-3"
        />
        <span id={`${id}-login-hint`} className="text-sm text-muted-foreground">
          {t('passkey.register.loginNameHint')}
        </span>
      </div>
      <div className="flex flex-col gap-1">
        <label htmlFor={`${id}-display`} className="text-sm font-medium">
          {t('passkey.register.displayName')}
        </label>
        <input
          id={`${id}-display`}
          name="nickname"
          required
          maxLength={64}
          autoComplete="nickname"
          value={displayName}
          aria-invalid={fieldError === 'display'}
          aria-describedby={`${id}-display-hint`}
          onChange={(e) => {
            setDisplayName(e.target.value)
          }}
          className="h-10 rounded-lg border bg-background px-3"
        />
        <span id={`${id}-display-hint`} className="text-sm text-muted-foreground">
          {t('passkey.register.displayNameHint')}
        </span>
      </div>
      {error !== null && (
        <p role="alert" className="text-destructive">
          {error.code === 'rate_limited' ? rateLimitMessage(error.retryAfter) : t(messageKey(error))}
        </p>
      )}
      <Button type="submit" size="lg" disabled={busy} className="self-start">
        {busy ? t('passkey.register.busy') : t('passkey.register.submit')}
      </Button>
    </form>
  )
}
