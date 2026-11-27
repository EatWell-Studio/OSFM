import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import zhCN from '@/i18n/locales/zh-CN.json'
import {
  groupSecret,
  isValidLoginName,
  normalizeLoginName,
  normalizeTotpCode,
  passwordLength,
} from '@/lib/passwordTotp'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'

// Made-up values only.
const invite = 'dGVzdC1pbnZpdGUtY29kZS1ub3QtYS1yZWFsLW9uZSE'
const secret = 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP'
const otpauthUri = `otpauth://totp/OSFM:test_new?algorithm=SHA1&digits=6&issuer=OSFM&period=30&secret=${secret}`
const me: Me = { uuid: '0190a1b2-0000-7000-8000-000000000009', displayName: 'Test New', role: 'member' }
const recoveryCodes = Array.from({ length: 10 }, (_, i) => `TEST-CODE-0000-000${String(i)}`)
const password = 'test password, made up'

type Handler = (body: unknown) => Response

/** Stubs fetch: GET /api/osfm/me answers 401 until signedIn() is true; POSTs by path from handlers. */
function stubApi(handlers: Record<string, Handler>, signedIn: () => boolean = () => false) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (path === '/api/osfm/me') {
      return Promise.resolve(signedIn() ? Response.json(me) : Response.json({}, { status: 401 }))
    }
    const handler = handlers[path]
    if (!handler || init?.method !== 'POST') {
      return Promise.resolve(Response.json({}, { status: 404 }))
    }
    return Promise.resolve(handler(JSON.parse(init.body as string)))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function authError(status: number, code: string) {
  return Response.json({ status, message: 'x', data: {}, code }, { status })
}

function rateLimited(retryAfter: string) {
  return Response.json(
    { status: 429, message: 'x', data: {}, code: 'rate_limited' },
    { status: 429, headers: { 'Retry-After': retryAfter } },
  )
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(<App router={router} queryClient={createQueryClient()} />)
  return router
}

afterEach(() => {
  sessionStorage.clear()
})

const reg = zhCN.pages.register.password
const login = zhCN.pages.login.password

describe('password and TOTP helpers', () => {
  it('normalises like the server', () => {
    expect(normalizeLoginName('  Test_New ')).toBe('test_new')
    expect(isValidLoginName('test_new')).toBe(true)
    expect(isValidLoginName('ab')).toBe(false)
    expect(isValidLoginName('test new')).toBe(false)
    expect(normalizeTotpCode(' 123 456 ')).toBe('123456')
    expect(normalizeTotpCode('12345')).toBeNull()
    expect(normalizeTotpCode('12345a')).toBeNull()
    expect(groupSecret('ABCDEFGHIJ')).toBe('ABCD EFGH IJ')
    // Full-width letters count as their ASCII forms after NFKC.
    expect(passwordLength('\uff54\uff45\uff53\uff54')).toBe(4)
  })
})

describe('registration with password and TOTP', () => {
  it('registers: account, authenticator app, recovery codes, then the map', async () => {
    const user = userEvent.setup()
    let registered = false
    const fetchMock = stubApi(
      {
        '/api/osfm/invites/check': () => Response.json({ valid: true }),
        '/api/osfm/register/password/start': () =>
          Response.json({ enrolment: 'sealed-enrolment', secret, otpauthUri, expiresAt: '2026-01-01T00:15:00Z' }),
        '/api/osfm/register/password/confirm': (body) => {
          if ((body as { code: string }).code !== '123456') {
            return authError(400, 'code_invalid')
          }
          registered = true
          return Response.json({ me, recoveryCodes }, { status: 201 })
        },
      },
      () => registered,
    )
    const router = renderAt(`/register#invite=${invite}`)

    await user.type(await screen.findByLabelText(reg.loginName), ' Test_New')
    await user.type(screen.getByLabelText(reg.displayName), 'Test New')
    await user.type(screen.getByLabelText(reg.password), password)
    await user.type(screen.getByLabelText(reg.repeat), password)
    await user.click(screen.getByRole('button', { name: reg.next }))

    // The authenticator step: QR code (inline SVG), the key, a link that opens the app.
    expect(await screen.findByRole('img', { name: reg.qrTitle })).toBeInstanceOf(SVGSVGElement)
    expect(screen.getByText(groupSecret(secret))).toBeInTheDocument()
    expect(screen.getByRole('link', { name: reg.openApp })).toHaveAttribute('href', otpauthUri)
    const start = fetchMock.mock.calls.find(([input]) => input === '/api/osfm/register/password/start')
    expect(new Headers(start?.[1]?.headers).get('X-OSFM')).toBe('1')
    expect(JSON.parse(start?.[1]?.body as string)).toEqual({
      invite,
      loginName: 'test_new',
      displayName: 'Test New',
      password,
    })

    // A wrong code, then the right one.
    await user.type(screen.getByLabelText(reg.code), '654321')
    await user.click(screen.getByRole('button', { name: reg.confirm }))
    expect(await screen.findByText(reg.errors.codeInvalid)).toHaveAttribute('role', 'alert')
    await user.clear(screen.getByLabelText(reg.code))
    await user.type(screen.getByLabelText(reg.code), '123 456')
    await user.click(screen.getByRole('button', { name: reg.confirm }))

    // The recovery codes: the map only after confirming they were saved.
    const list = await screen.findByRole('list', { name: zhCN.recoveryCodes.title })
    expect(
      within(list)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(recoveryCodes)
    const go = screen.getByRole('button', { name: zhCN.recoveryCodes.continue })
    expect(go).toBeDisabled()
    expect(router.state.location.pathname).toBe('/register')
    await user.click(screen.getByLabelText(zhCN.recoveryCodes.confirm))
    await user.click(go)
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/')
    })
    expect(sessionStorage.getItem('osfm.inviteCode')).toBeNull()
  })

  it('says how long to wait when the invite check or the registration was refused for too many attempts', async () => {
    const user = userEvent.setup()
    let checkAnswer = () => rateLimited('30')
    stubApi({
      '/api/osfm/invites/check': () => checkAnswer(),
      '/api/osfm/register/password/start': () => rateLimited('120'),
    })
    sessionStorage.setItem('osfm.inviteCode', invite)
    const { unmount } = render(
      <App router={createMemoryRouter(routes, { initialEntries: ['/register'] })} queryClient={createQueryClient()} />,
    )
    expect(await screen.findByText(zhCN.rateLimit.wait_other.replace('{{count}}', '1'))).toBeInTheDocument()
    unmount()

    checkAnswer = () => Response.json({ valid: true })
    renderAt('/register')
    await user.type(await screen.findByLabelText(reg.loginName), 'test_new')
    await user.type(screen.getByLabelText(reg.password), password)
    await user.type(screen.getByLabelText(reg.repeat), password)
    await user.click(screen.getByRole('button', { name: reg.next }))
    expect(await screen.findByText(zhCN.rateLimit.wait_other.replace('{{count}}', '2'))).toBeInTheDocument()
  })

  it('checks the form before sending it, and shows the server errors at their field', async () => {
    const user = userEvent.setup()
    const fetchMock = stubApi({
      '/api/osfm/invites/check': () => Response.json({ valid: true }),
      '/api/osfm/register/password/start': () => authError(409, 'login_name_taken'),
    })
    renderAt(`/register#invite=${invite}`)

    await user.type(await screen.findByLabelText(reg.loginName), 'ab')
    await user.type(screen.getByLabelText(reg.password), 'short')
    await user.click(screen.getByRole('button', { name: reg.next }))
    expect(screen.getByText(reg.errors.loginNameInvalid)).toBeInTheDocument()
    expect(screen.getByText(reg.errors.passwordTooShort)).toBeInTheDocument()

    await user.type(screen.getByLabelText(reg.loginName), 'c')
    await user.clear(screen.getByLabelText(reg.password))
    await user.type(screen.getByLabelText(reg.password), password)
    await user.type(screen.getByLabelText(reg.repeat), password + 'x')
    await user.click(screen.getByRole('button', { name: reg.next }))
    expect(screen.getByText(reg.errors.passwordsDiffer)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => input === '/api/osfm/register/password/start')).toBe(false)

    await user.clear(screen.getByLabelText(reg.repeat))
    await user.type(screen.getByLabelText(reg.repeat), password)
    await user.click(screen.getByRole('button', { name: reg.next }))
    expect(await screen.findByText(reg.errors.loginNameTaken)).toHaveAttribute('role', 'alert')
    expect(screen.getByLabelText(reg.loginName)).toHaveAttribute('aria-invalid', 'true')
  })
})

describe('sign-in with password', () => {
  it('signs in with password and TOTP code and returns to the page asked for', async () => {
    const user = userEvent.setup()
    let signedIn = false
    const fetchMock = stubApi(
      {
        '/api/osfm/signin/password': (body) =>
          (body as { password: string }).password === password
            ? Response.json({ ticket: 'sealed-ticket', expiresAt: '2026-01-01T00:05:00Z' })
            : authError(401, 'credentials_invalid'),
        '/api/osfm/signin/totp': () => {
          signedIn = true
          return Response.json({ me })
        },
      },
      () => signedIn,
    )
    const router = renderAt('/login?next=%2Flist')

    await user.type(await screen.findByLabelText(login.loginName), 'Test_New')
    await user.type(screen.getByLabelText(login.password), 'wrong password')
    await user.click(screen.getByRole('button', { name: login.next }))
    expect(await screen.findByText(login.errors.credentials)).toHaveAttribute('role', 'alert')
    expect(screen.getByLabelText(login.password)).toHaveValue('')

    await user.type(screen.getByLabelText(login.password), password)
    await user.click(screen.getByRole('button', { name: login.next }))
    await user.type(await screen.findByLabelText(login.totp), '123456')
    await user.click(screen.getByRole('button', { name: login.signIn }))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/list')
    })
    const step2 = fetchMock.mock.calls.find(([input]) => input === '/api/osfm/signin/totp')
    expect(JSON.parse(step2?.[1]?.body as string)).toEqual({ ticket: 'sealed-ticket', code: '123456' })
    const step1 = fetchMock.mock.calls.find(([input]) => input === '/api/osfm/signin/password')
    expect(JSON.parse(step1?.[1]?.body as string)).toMatchObject({ loginName: 'test_new' })
  })

  it('signs in with a recovery code and says how many are left', async () => {
    const user = userEvent.setup()
    let signedIn = false
    stubApi(
      {
        '/api/osfm/signin/password': () => Response.json({ ticket: 'sealed-ticket', expiresAt: 'x' }),
        '/api/osfm/signin/recovery': (body) => {
          if ((body as { code: string }).code !== 'test-code-0000-0001') {
            return authError(401, 'code_invalid')
          }
          signedIn = true
          return Response.json({ me, recoveryCodesLeft: 9 })
        },
      },
      () => signedIn,
    )
    const router = renderAt('/login')
    await user.type(await screen.findByLabelText(login.loginName), 'test_new')
    await user.type(screen.getByLabelText(login.password), password)
    await user.click(screen.getByRole('button', { name: login.next }))
    await user.click(await screen.findByRole('button', { name: login.useRecovery }))
    await user.type(screen.getByLabelText(login.recovery), 'used-code')
    await user.click(screen.getByRole('button', { name: login.signIn }))
    expect(await screen.findByText(login.errors.recoveryInvalid)).toHaveAttribute('role', 'alert')
    await user.clear(screen.getByLabelText(login.recovery))
    await user.type(screen.getByLabelText(login.recovery), 'test-code-0000-0001')
    await user.click(screen.getByRole('button', { name: login.signIn }))
    expect(await screen.findByRole('status')).toHaveTextContent('9')
    await user.click(screen.getByRole('button', { name: login.continue }))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/')
    })
  })

  it('starts over when the ticket has expired', async () => {
    const user = userEvent.setup()
    stubApi({
      '/api/osfm/signin/password': () => Response.json({ ticket: 'sealed-ticket', expiresAt: 'x' }),
      '/api/osfm/signin/totp': () => authError(401, 'ticket_expired'),
    })
    renderAt('/login')
    await user.type(await screen.findByLabelText(login.loginName), 'test_new')
    await user.type(screen.getByLabelText(login.password), password)
    await user.click(screen.getByRole('button', { name: login.next }))
    await user.type(await screen.findByLabelText(login.totp), '123456')
    await user.click(screen.getByRole('button', { name: login.signIn }))
    expect(await screen.findByText(login.errors.expired)).toBeInTheDocument()
    expect(screen.getByLabelText(login.loginName)).toHaveValue('test_new')
  })

  it('says how long to wait after too many attempts, at either step', async () => {
    const user = userEvent.setup()
    let passwordAnswer = () => rateLimited('61')
    stubApi({
      '/api/osfm/signin/password': () => passwordAnswer(),
      '/api/osfm/signin/totp': () => rateLimited('3600'),
    })
    renderAt('/login')
    await user.type(await screen.findByLabelText(login.loginName), 'test_new')
    await user.type(screen.getByLabelText(login.password), password)
    await user.click(screen.getByRole('button', { name: login.next }))
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.rateLimit.wait_other.replace('{{count}}', '2'))

    passwordAnswer = () => Response.json({ ticket: 'sealed-ticket', expiresAt: 'x' })
    await user.type(screen.getByLabelText(login.password), password)
    await user.click(screen.getByRole('button', { name: login.next }))
    await user.type(await screen.findByLabelText(login.totp), '123456')
    await user.click(screen.getByRole('button', { name: login.signIn }))
    expect(await screen.findByText(zhCN.rateLimit.wait_other.replace('{{count}}', '60'))).toBeInTheDocument()
  })
})
