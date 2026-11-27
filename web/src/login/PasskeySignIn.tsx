import { KeyRound } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  assertionToJSON,
  PasskeyError,
  passkeyErrorFrom,
  passkeysSupported,
  postJson,
  readCeremonyStart,
  requestOptionsFromJSON,
} from '@/lib/webauthn'
import { rateLimitMessage } from '@/lib/rateLimit'

const messageKeys = {
  passkey_failed: 'passkey.signIn.errors.failed',
  passkeys_unavailable: 'passkey.errors.unavailable',
  cancelled: 'passkey.errors.cancelled',
  exists: 'passkey.errors.failed',
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
 * Sign-in with a passkey (D4): no login name, the browser offers the passkeys it has for this instance. onSignedIn is
 * called once the server has started the session.
 */
export function PasskeySignIn({ onSignedIn }: { onSignedIn: () => void }) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<PasskeyError | null>(null)

  if (!passkeysSupported()) {
    return (
      <p role="alert" className="text-muted-foreground">
        {t('passkey.errors.unsupported')}
      </p>
    )
  }

  async function signIn() {
    const start = await readCeremonyStart(await postJson('/passkey/login/options'))
    let credential: Credential | null
    try {
      credential = await navigator.credentials.get(requestOptionsFromJSON(start.options))
    } catch (e) {
      throw passkeyErrorFrom(e)
    }
    if (!(credential instanceof PublicKeyCredential)) {
      throw new PasskeyError('failed')
    }
    await postJson('/passkey/login', { state: start.state, credential: assertionToJSON(credential) })
    onSignedIn()
  }

  return (
    <div className="flex flex-col gap-3">
      <Button
        size="lg"
        disabled={busy}
        className="self-start"
        onClick={() => {
          setBusy(true)
          setError(null)
          signIn()
            .catch((err: unknown) => {
              setError(passkeyErrorFrom(err))
            })
            .finally(() => {
              setBusy(false)
            })
        }}
      >
        <KeyRound aria-hidden="true" />
        {busy ? t('passkey.signIn.busy') : t('passkey.signIn.button')}
      </Button>
      {error !== null && (
        <p role="alert" className="text-destructive">
          {error.code === 'rate_limited' ? rateLimitMessage(t, error.retryAfter) : t(messageKey(error))}
        </p>
      )}
    </div>
  )
}
