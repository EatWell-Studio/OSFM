// Passkeys with the browser's WebAuthn API (docs/architecture.md §3.3). The server sends the options in their JSON form
// (binary values base64url-encoded) and expects the resulting credential in the same form. The conversion is done here
// rather than with PublicKeyCredential.parseCreationOptionsFromJSON() and toJSON(), which older browsers still in use
// (N6) do not have.

import { apiFetch, isJson, retryAfterSeconds } from '@/lib/api'

/** Whether this browser can use passkeys at all; without it, members register with a password and TOTP. */
export function passkeysSupported(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.isSecureContext &&
    typeof window.PublicKeyCredential === 'function' &&
    'credentials' in navigator &&
    typeof navigator.credentials.create === 'function' &&
    typeof navigator.credentials.get === 'function'
  )
}

export function base64urlToBuffer(value: string): ArrayBuffer {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64 + '='.repeat((4 - (base64.length % 4)) % 4)
  const binary = atob(padded)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i)
  }
  return bytes.buffer
}

export function bufferToBase64url(buffer: ArrayBuffer | ArrayBufferView): string {
  const bytes =
    buffer instanceof ArrayBuffer
      ? new Uint8Array(buffer)
      : new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength)
  let binary = ''
  for (const b of bytes) {
    binary += String.fromCharCode(b)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

type Json = Record<string, unknown>

function isObject(v: unknown): v is Json {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function descriptors(list: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(list)) {
    return undefined
  }
  return list.filter(isObject).map((d) => ({
    ...(d as unknown as PublicKeyCredentialDescriptor),
    id: base64urlToBuffer(String(d.id)),
  }))
}

/** The CredentialCreationOptions for navigator.credentials.create() from their JSON form ({"publicKey": {…}}). */
export function creationOptionsFromJSON(options: unknown): CredentialCreationOptions {
  if (!isObject(options) || !isObject(options.publicKey)) {
    throw new Error('webauthn: unexpected creation options')
  }
  const pk = options.publicKey
  if (typeof pk.challenge !== 'string' || !isObject(pk.user) || typeof pk.user.id !== 'string') {
    throw new Error('webauthn: unexpected creation options')
  }
  const publicKey = {
    ...(pk as unknown as PublicKeyCredentialCreationOptions),
    challenge: base64urlToBuffer(pk.challenge),
    user: { ...(pk.user as unknown as PublicKeyCredentialUserEntity), id: base64urlToBuffer(pk.user.id) },
    excludeCredentials: descriptors(pk.excludeCredentials),
  }
  return { publicKey }
}

/** The CredentialRequestOptions for navigator.credentials.get() from their JSON form. */
export function requestOptionsFromJSON(options: unknown): CredentialRequestOptions {
  if (!isObject(options) || !isObject(options.publicKey) || typeof options.publicKey.challenge !== 'string') {
    throw new Error('webauthn: unexpected request options')
  }
  const pk = options.publicKey
  const publicKey = {
    ...(pk as unknown as PublicKeyCredentialRequestOptions),
    challenge: base64urlToBuffer(String(pk.challenge)),
    allowCredentials: descriptors(pk.allowCredentials),
  }
  return { publicKey }
}

/** The JSON form (RegistrationResponseJSON) of a credential from navigator.credentials.create(). */
export function registrationToJSON(credential: PublicKeyCredential): Json {
  const response = credential.response as AuthenticatorAttestationResponse
  const transports = typeof response.getTransports === 'function' ? response.getTransports() : []
  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    response: {
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
      attestationObject: bufferToBase64url(response.attestationObject),
      transports,
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  }
}

/** The JSON form (AuthenticationResponseJSON) of a credential from navigator.credentials.get(). */
export function assertionToJSON(credential: PublicKeyCredential): Json {
  const response = credential.response as AuthenticatorAssertionResponse
  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    response: {
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
      authenticatorData: bufferToBase64url(response.authenticatorData),
      signature: bufferToBase64url(response.signature),
      userHandle: response.userHandle ? bufferToBase64url(response.userHandle) : undefined,
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  }
}

/** Why a passkey ceremony did not complete, as the pages explain it. */
export type PasskeyFailure =
  | 'cancelled' // the member cancelled, or the browser's time ran out (NotAllowedError)
  | 'exists' // the authenticator has a passkey for this account already (InvalidStateError)
  | 'address' // the page's address cannot use passkeys of this instance (SecurityError, e.g. http://127.0.0.1)
  | 'unsupported' // the browser or device cannot create such a passkey (NotSupportedError)
  | 'failed' // anything else, the server's refusal included

/** An error with the reason a ceremony failed and, for a refusal by the server, its error code. */
export class PasskeyError extends Error {
  readonly reason: PasskeyFailure
  readonly code: string | undefined
  /** For rate_limited: the seconds to wait, from the Retry-After header. */
  readonly retryAfter: number | null

  constructor(reason: PasskeyFailure, code?: string, retryAfter: number | null = null) {
    super(`passkey: ${reason}${code ? ` (${code})` : ''}`)
    this.name = 'PasskeyError'
    this.reason = reason
    this.code = code
    this.retryAfter = retryAfter
  }
}

/** The PasskeyError for an exception of navigator.credentials. */
export function passkeyErrorFrom(e: unknown): PasskeyError {
  if (e instanceof PasskeyError) {
    return e
  }
  const name = e instanceof Error || e instanceof DOMException ? e.name : ''
  switch (name) {
    case 'NotAllowedError':
    case 'AbortError':
      return new PasskeyError('cancelled')
    case 'InvalidStateError':
      return new PasskeyError('exists')
    case 'SecurityError':
      return new PasskeyError('address')
    case 'NotSupportedError':
      return new PasskeyError('unsupported')
    default:
      return new PasskeyError('failed')
  }
}

/** Sends a JSON POST to the OSFM API; an unsuccessful response becomes a PasskeyError with the server's code. */
export async function postJson(path: string, body?: unknown): Promise<Response> {
  const response = await apiFetch(path, {
    method: 'POST',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) {
    let code: string | undefined
    if (isJson(response)) {
      const data: unknown = await response.json().catch(() => null)
      if (isObject(data) && typeof data.code === 'string') {
        code = data.code
      }
    }
    throw new PasskeyError('failed', code ?? `http_${String(response.status)}`, retryAfterSeconds(response))
  }
  return response
}

/** The options and sealed state of the first step of a ceremony. */
export interface CeremonyStart {
  options: unknown
  state: string
}

export async function readCeremonyStart(response: Response): Promise<CeremonyStart> {
  const data: unknown = await response.json()
  if (!isObject(data) || typeof data.state !== 'string' || !isObject(data.options)) {
    throw new PasskeyError('failed')
  }
  return { options: data.options, state: data.state }
}
