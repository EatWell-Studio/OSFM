import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import zhCN from '@/i18n/locales/zh-CN.json'
import { PasskeySignIn } from '@/login/PasskeySignIn'
import { PasskeyRegistration } from '@/register/PasskeyRegistration'
import {
  assertionToJSON,
  base64urlToBuffer,
  bufferToBase64url,
  creationOptionsFromJSON,
  PasskeyError,
  passkeyErrorFrom,
  registrationToJSON,
  requestOptionsFromJSON,
} from './webauthn'

// Made-up values only.
const bytes = (...b: number[]) => new Uint8Array(b).buffer
const creationJSON = {
  publicKey: {
    rp: { id: 'localhost', name: 'OSFM' },
    user: { id: 'AQID', name: 'test_new', displayName: 'Test New' },
    challenge: '_-8',
    pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
    authenticatorSelection: { residentKey: 'required', userVerification: 'required' },
    excludeCredentials: [{ type: 'public-key', id: 'BAU' }],
  },
}
const requestJSON = { publicKey: { challenge: 'AAEC', rpId: 'localhost', userVerification: 'required' } }

/** A PublicKeyCredential class for jsdom, which has none, and an instance of it. */
class FakePublicKeyCredential {
  id = 'Bwg'
  rawId = bytes(7, 8)
  type = 'public-key'
  authenticatorAttachment = 'platform'
  response: Record<string, unknown>
  constructor(response: Record<string, unknown>) {
    this.response = response
  }
  getClientExtensionResults() {
    return {}
  }
}

function stubWebAuthn(credential: { create?: () => Promise<unknown>; get?: () => Promise<unknown> }) {
  vi.stubGlobal('PublicKeyCredential', FakePublicKeyCredential)
  vi.stubGlobal('isSecureContext', true)
  Object.defineProperty(navigator, 'credentials', {
    configurable: true,
    value: { create: vi.fn(credential.create), get: vi.fn(credential.get) },
  })
}

type Handler = (body: unknown) => Response
function stubFetch(handlers: Record<string, Handler>) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const handler = handlers[path]
    const body: unknown = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined
    return Promise.resolve(handler ? handler(body) : new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('webauthn conversions', () => {
  it('round-trips base64url', () => {
    const b = bytes(0, 255, 254, 1, 2)
    expect(bufferToBase64url(b)).toBe('AP_-AQI')
    expect(new Uint8Array(base64urlToBuffer('AP_-AQI'))).toEqual(new Uint8Array(b))
  })

  it('decodes the binary members of the options', () => {
    const { publicKey } = creationOptionsFromJSON(creationJSON)
    expect(new Uint8Array(publicKey?.user.id as ArrayBuffer)).toEqual(new Uint8Array([1, 2, 3]))
    expect(new Uint8Array(publicKey?.challenge as ArrayBuffer)).toEqual(new Uint8Array([255, 239]))
    expect(new Uint8Array(publicKey?.excludeCredentials?.[0]?.id as ArrayBuffer)).toEqual(new Uint8Array([4, 5]))
    expect(publicKey?.authenticatorSelection?.residentKey).toBe('required')
    const request = requestOptionsFromJSON(requestJSON).publicKey
    expect(new Uint8Array(request?.challenge as ArrayBuffer)).toEqual(new Uint8Array([0, 1, 2]))
    expect(() => creationOptionsFromJSON({})).toThrow()
  })

  it('encodes the credentials', () => {
    const created = new FakePublicKeyCredential({
      clientDataJSON: bytes(1),
      attestationObject: bytes(2),
      getTransports: () => ['internal'],
    }) as unknown as PublicKeyCredential
    expect(registrationToJSON(created)).toEqual({
      id: 'Bwg',
      rawId: 'Bwg',
      type: 'public-key',
      authenticatorAttachment: 'platform',
      response: { clientDataJSON: 'AQ', attestationObject: 'Ag', transports: ['internal'] },
      clientExtensionResults: {},
    })
    const asserted = new FakePublicKeyCredential({
      clientDataJSON: bytes(1),
      authenticatorData: bytes(2),
      signature: bytes(3),
      userHandle: bytes(4),
    }) as unknown as PublicKeyCredential
    expect(assertionToJSON(asserted)).toMatchObject({
      response: { clientDataJSON: 'AQ', authenticatorData: 'Ag', signature: 'Aw', userHandle: 'BA' },
    })
  })

  it('classifies browser errors', () => {
    expect(passkeyErrorFrom(new DOMException('', 'NotAllowedError')).reason).toBe('cancelled')
    expect(passkeyErrorFrom(new DOMException('', 'SecurityError')).reason).toBe('address')
    expect(passkeyErrorFrom(new DOMException('', 'InvalidStateError')).reason).toBe('exists')
    expect(passkeyErrorFrom(new TypeError('x')).reason).toBe('failed')
    const e = new PasskeyError('failed', 'login_name_taken')
    expect(passkeyErrorFrom(e)).toBe(e)
  })
})

describe('passkey sign-in', () => {
  it('explains when the browser has no passkey support', () => {
    vi.stubGlobal('PublicKeyCredential', undefined)
    render(<PasskeySignIn onSignedIn={vi.fn()} />)
    expect(screen.getByRole('alert')).toHaveTextContent(zhCN.passkey.errors.unsupported)
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('signs in with the credential the browser returns', async () => {
    const get = () =>
      Promise.resolve(
        new FakePublicKeyCredential({
          clientDataJSON: bytes(1),
          authenticatorData: bytes(2),
          signature: bytes(3),
          userHandle: bytes(4),
        }),
      )
    stubWebAuthn({ get })
    let finish: unknown
    const fetchMock = stubFetch({
      '/api/osfm/passkey/login/options': () => Response.json({ options: requestJSON, state: 'test-state' }),
      '/api/osfm/passkey/login': (body) => {
        finish = body
        return new Response(null, { status: 204 })
      },
    })
    const onSignedIn = vi.fn()
    render(<PasskeySignIn onSignedIn={onSignedIn} />)
    await userEvent.click(screen.getByRole('button', { name: zhCN.passkey.signIn.button }))
    await waitFor(() => {
      expect(onSignedIn).toHaveBeenCalled()
    })
    expect(finish).toMatchObject({ state: 'test-state', credential: { id: 'Bwg', response: { userHandle: 'BA' } } })
    for (const [, init] of fetchMock.mock.calls) {
      expect(new Headers(init?.headers).get('X-OSFM')).toBe('1')
    }
  })

  it('says so when the member cancels, and when the server refuses', async () => {
    stubWebAuthn({ get: () => Promise.reject(new DOMException('', 'NotAllowedError')) })
    stubFetch({
      '/api/osfm/passkey/login/options': () => Response.json({ options: requestJSON, state: 'test-state' }),
    })
    render(<PasskeySignIn onSignedIn={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: zhCN.passkey.signIn.button }))
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.passkey.errors.cancelled)

    stubWebAuthn({
      get: () =>
        Promise.resolve(
          new FakePublicKeyCredential({ clientDataJSON: bytes(1), authenticatorData: bytes(2), signature: bytes(3) }),
        ),
    })
    stubFetch({
      '/api/osfm/passkey/login/options': () => Response.json({ options: requestJSON, state: 'test-state' }),
      '/api/osfm/passkey/login': () => Response.json({ status: 401, code: 'passkey_failed' }, { status: 401 }),
    })
    await userEvent.click(screen.getByRole('button', { name: zhCN.passkey.signIn.button }))
    expect(await screen.findByText(zhCN.passkey.signIn.errors.failed)).toHaveAttribute('role', 'alert')
  })
})

describe('passkey registration', () => {
  it('creates the passkey, shows the recovery codes and waits for the confirmation', async () => {
    const create = () =>
      Promise.resolve(
        new FakePublicKeyCredential({
          clientDataJSON: bytes(1),
          attestationObject: bytes(2),
          getTransports: () => ['internal'],
        }),
      )
    stubWebAuthn({ create })
    const bodies: unknown[] = []
    const codes = Array.from({ length: 10 }, (_, i) => `0000-0000-0000-0000-0000-0000-0000-000${String(i)}`)
    stubFetch({
      '/api/osfm/passkey/register/options': (body) => {
        bodies.push(body)
        return Response.json({ options: creationJSON, state: 'test-state' })
      },
      '/api/osfm/passkey/register': (body) => {
        bodies.push(body)
        return Response.json({ recoveryCodes: codes }, { status: 201 })
      },
    })
    const onDone = vi.fn()
    render(<PasskeyRegistration code="test-code" onDone={onDone} />)
    await userEvent.type(screen.getByLabelText(zhCN.passkey.register.loginName), 'test_new')
    await userEvent.type(screen.getByLabelText(zhCN.passkey.register.displayName), 'Test New')
    await userEvent.click(screen.getByRole('button', { name: zhCN.passkey.register.submit }))

    expect(await screen.findByText(codes[9] ?? '')).toBeInTheDocument()
    expect(bodies[0]).toEqual({ code: 'test-code', loginName: 'test_new', displayName: 'Test New' })
    expect(bodies[1]).toMatchObject({ code: 'test-code', state: 'test-state', credential: { rawId: 'Bwg' } })
    const go = screen.getByRole('button', { name: zhCN.recoveryCodes.continue })
    expect(go).toBeDisabled()
    await userEvent.click(screen.getByLabelText(zhCN.recoveryCodes.confirm))
    await userEvent.click(go)
    expect(onDone).toHaveBeenCalled()
  })

  it('shows the server’s reason', async () => {
    stubWebAuthn({})
    stubFetch({
      '/api/osfm/passkey/register/options': () =>
        Response.json({ status: 409, code: 'login_name_taken' }, { status: 409 }),
    })
    render(<PasskeyRegistration code="test-code" onDone={vi.fn()} />)
    await userEvent.type(screen.getByLabelText(zhCN.passkey.register.loginName), 'test_taken')
    await userEvent.click(screen.getByRole('button', { name: zhCN.passkey.register.submit }))
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.passkey.register.errors.loginNameTaken)
    expect(screen.getByLabelText(zhCN.passkey.register.loginName)).toHaveAttribute('aria-invalid', 'true')
  })
})
