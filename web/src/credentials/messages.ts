import { i18n } from '@/i18n'
import { rateLimitedCode } from '@/lib/authApi'
import { rateLimitMessage } from '@/lib/rateLimit'
import { PasskeyError } from '@/lib/webauthn'
import { errorCode, errorRetryAfter } from './api'

// The message for a failed request on the credential pages, by the server's error code or, for a passkey ceremony
// that did not complete, by why the browser stopped it.
const byCode = {
  invalid_request: 'credentials.errors.network',
  password_too_short: 'register.password.errors.passwordTooShort',
  password_too_long: 'register.password.errors.passwordTooLong',
  password_is_login_name: 'register.password.errors.passwordIsLoginName',
  code_invalid: 'credentials.errors.codeInvalid',
  credentials_invalid: 'credentials.errors.passwordWrong',
  enrolment_expired: 'credentials.errors.expired',
  last_credential: 'credentials.errors.lastCredential',
  credential_not_found: 'credentials.errors.notFound',
  no_password: 'credentials.errors.noPassword',
  no_passkey: 'credentials.errors.noPasskey',
  password_exists: 'credentials.errors.passwordExists',
  device_name_invalid: 'credentials.errors.deviceName',
  reauth_required: 'credentials.verify.required',
  recovery_failed: 'credentials.recover.errors.failed',
  recovery_use_password: 'credentials.recover.errors.usePassword',
  recovery_expired: 'credentials.recover.errors.expired',
  reset_invalid: 'credentials.reset.invalid',
  passkey_failed: 'passkey.register.errors.verifyFailed',
  passkeys_unavailable: 'passkey.errors.unavailable',
} as const

const byReason = {
  cancelled: 'passkey.errors.cancelled',
  exists: 'passkey.register.errors.exists',
  address: 'passkey.errors.address',
  unsupported: 'passkey.errors.unsupported',
  failed: 'passkey.errors.failed',
} as const

export function credentialErrorMessage(error: unknown): string {
  const code = errorCode(error)
  if (code === rateLimitedCode) {
    return rateLimitMessage(errorRetryAfter(error))
  }
  if (code !== null && code in byCode) {
    return i18n.t(byCode[code as keyof typeof byCode], { ns: 'auth' })
  }
  if (error instanceof PasskeyError) {
    return i18n.t(byReason[error.reason], { ns: 'auth' })
  }
  return i18n.t('credentials.errors.network', { ns: 'auth' })
}
