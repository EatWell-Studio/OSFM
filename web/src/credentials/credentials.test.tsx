import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import zhCN from '@/i18n/locales/zh-CN.json'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'
import { type Credentials, isFresh, resetTokenFromHash } from './api'

// Made-up values only.
const member: Me = { uuid: '0190a1b2-0000-7000-8000-000000000002', displayName: 'Test Member', role: 'member' }
const maintainer: Me = { ...member, displayName: 'Test Maintainer', role: 'maintainer' }
const token = 'dGVzdC1yZXNldC10b2tlbi1ub3QtYS1yZWFsLW9uZSE'
const codes = Array.from({ length: 10 }, (_, i) => `TEST-CODE-0000-000${String(i)}`)
const later = new Date(Date.now() + 9 * 60_000).toISOString()

function credentials(overrides: Partial<Credentials> = {}): Credentials {
  return {
    passkeys: [
      {
        uuid: '0190a1b2-0000-7000-8000-00000000c001',
        deviceName: 'Test Phone',
        createdAt: '2026-01-01T10:00:00Z',
        lastUsedAt: '2026-01-02T00:00:00Z',
      },
    ],
    passwordTotp: null,
    recoveryCodesLeft: 7,
    verifiedUntil: later,
    ...overrides,
  }
}

type Handler = (init: RequestInit | undefined) => Response | Promise<Response>

/** Stubs fetch: /api/osfm/me answers with me (401 for null); other paths by "METHOD path" from handlers. */
function stubApi(me: Me | null, handlers: Record<string, Handler> = {}) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const method = init?.method ?? 'GET'
    if (path === '/api/osfm/me') {
      return Promise.resolve(me ? Response.json(me) : Response.json({}, { status: 401 }))
    }
    const handler = handlers[`${method} ${path}`]
    return Promise.resolve(handler ? handler(init) : new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(<App router={router} queryClient={createQueryClient()} />)
  return router
}

function body(init: RequestInit | undefined): unknown {
  return typeof init?.body === 'string' ? (JSON.parse(init.body) as unknown) : null
}

afterEach(() => {
  vi.unstubAllGlobals()
  sessionStorage.clear()
})

describe('helpers', () => {
  it('reads a reset token from the fragment', () => {
    expect(resetTokenFromHash(`#token=${token}`)).toBe(token)
    expect(resetTokenFromHash('#token=x')).toBeNull()
    expect(resetTokenFromHash('')).toBeNull()
  })

  it('tells a fresh session from a stale one', () => {
    expect(isFresh(credentials())).toBe(true)
    expect(isFresh(credentials({ verifiedUntil: null }))).toBe(false)
    expect(isFresh(credentials({ verifiedUntil: new Date(Date.now() + 30_000).toISOString() }))).toBe(false)
  })
})

describe('the me page', () => {
  it('lists the credentials, keeps the last one and regenerates recovery codes', async () => {
    const fetchMock = stubApi(member, {
      'GET /api/osfm/me/credentials': () => Response.json(credentials()),
      'POST /api/osfm/me/recovery-codes': () => Response.json({ recoveryCodes: codes }, { status: 201 }),
    })
    renderAt('/me')
    const list = await screen.findByRole('list', { name: zhCN.credentials.passkeys.title })
    expect(within(list).getByText('Test Phone')).toBeInTheDocument()
    // The only passkey of a member without a password cannot be removed.
    expect(within(list).getByRole('button', { name: zhCN.credentials.remove })).toBeDisabled()
    expect(screen.getAllByText(zhCN.credentials.lastCredentialHint).length).toBeGreaterThan(0)
    expect(screen.getByRole('button', { name: zhCN.credentials.password.add })).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: zhCN.credentials.recovery.regenerate }))
    expect(await screen.findByText(codes[0] ?? '')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/osfm/me/recovery-codes', expect.objectContaining({ method: 'POST' }))
  })

  it('asks for a re-verification first when the session is stale, then does the change', async () => {
    const fetchMock = stubApi(member, {
      'GET /api/osfm/me/credentials': () =>
        Response.json(
          credentials({ verifiedUntil: null, passwordTotp: { createdAt: '2026-01-01T00:00:00Z', lastUsedAt: null } }),
        ),
      'POST /api/osfm/me/verify': () => Response.json({ verifiedUntil: later }),
      'POST /api/osfm/me/recovery-codes': () => Response.json({ recoveryCodes: codes }, { status: 201 }),
    })
    renderAt('/me')
    await userEvent.click(await screen.findByRole('button', { name: zhCN.credentials.recovery.regenerate }))
    expect(await screen.findByRole('heading', { name: zhCN.credentials.verify.title })).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalledWith('/api/osfm/me/recovery-codes', expect.anything())
    await userEvent.type(screen.getByLabelText(zhCN.pages.login.password.password), 'test password')
    await userEvent.type(screen.getByLabelText(zhCN.credentials.verify.code), '123456')
    await userEvent.click(screen.getByRole('button', { name: zhCN.credentials.verify.submit }))
    expect(await screen.findByText(codes[0] ?? '')).toBeInTheDocument()
    const verify = fetchMock.mock.calls.find(([p]) => p === '/api/osfm/me/verify')
    expect(body(verify?.[1])).toEqual({ password: 'test password', code: '123456' })
  })

  it('shows the verification when the server asks for it', async () => {
    stubApi(member, {
      'GET /api/osfm/me/credentials': () =>
        Response.json(credentials({ passwordTotp: { createdAt: '2026-01-01T00:00:00Z', lastUsedAt: null } })),
      'POST /api/osfm/me/password-totp/remove': () =>
        Response.json({ status: 403, code: 'reauth_required', message: '', data: {} }, { status: 403 }),
    })
    renderAt('/me')
    const remove = await screen.findAllByRole('button', { name: zhCN.credentials.remove })
    // eslint-disable-next-line @typescript-eslint/no-non-null-assertion -- found above
    await userEvent.click(remove[0]!)
    await userEvent.click(screen.getByRole('button', { name: zhCN.credentials.confirmRemove }))
    expect(await screen.findByText(zhCN.credentials.verify.repeat)).toBeInTheDocument()
  })
})

describe('recovery on the sign-in page', () => {
  it('turns a recovery code into a new credential before signing in', async () => {
    const fetchMock = stubApi(null, {
      'POST /api/osfm/recover': () => Response.json({ ticket: 'test-ticket', expiresAt: '2026-01-01T00:15:00Z' }),
    })
    renderAt('/login')
    await userEvent.click(await screen.findByRole('button', { name: zhCN.credentials.recover.entry }))
    await userEvent.type(screen.getByLabelText(zhCN.pages.login.password.loginName), ' Test_Member ')
    await userEvent.type(screen.getByLabelText(zhCN.pages.login.password.recovery), 'AAAA-BBBB-CCCC-DDDD')
    await userEvent.click(screen.getByRole('button', { name: zhCN.pages.login.password.next }))
    expect(await screen.findByText(zhCN.credentials.recover.enrol)).toBeInTheDocument()
    const call = fetchMock.mock.calls.find(([p]) => p === '/api/osfm/recover')
    expect(body(call?.[1])).toEqual({ loginName: 'test_member', code: 'AAAA-BBBB-CCCC-DDDD' })
    // The password method comes first.
    expect(screen.getByRole('button', { name: zhCN.pages.register.methods.password })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })

  it('says the same for every failure', async () => {
    stubApi(null, {
      'POST /api/osfm/recover': () =>
        Response.json({ status: 401, code: 'recovery_failed', message: '', data: {} }, { status: 401 }),
    })
    renderAt('/login')
    await userEvent.click(await screen.findByRole('button', { name: zhCN.credentials.recover.entry }))
    await userEvent.type(screen.getByLabelText(zhCN.pages.login.password.loginName), 'test_member')
    await userEvent.type(screen.getByLabelText(zhCN.pages.login.password.recovery), 'x')
    await userEvent.click(screen.getByRole('button', { name: zhCN.pages.login.password.next }))
    expect(await screen.findByText(zhCN.credentials.recover.errors.failed)).toBeInTheDocument()
  })

  it('shows the wait after too many attempts', async () => {
    stubApi(null, {
      'POST /api/osfm/recover': () =>
        Response.json(
          { status: 429, code: 'rate_limited', message: '', data: {} },
          { status: 429, headers: { 'Retry-After': '120' } },
        ),
    })
    renderAt('/login')
    await userEvent.click(await screen.findByRole('button', { name: zhCN.credentials.recover.entry }))
    await userEvent.type(screen.getByLabelText(zhCN.pages.login.password.loginName), 'test_member')
    await userEvent.type(screen.getByLabelText(zhCN.pages.login.password.recovery), 'x')
    await userEvent.click(screen.getByRole('button', { name: zhCN.pages.login.password.next }))
    expect(await screen.findByText(zhCN.rateLimit.wait_other.replace('{{count}}', '2'))).toBeInTheDocument()
  })
})

describe('the reset page', () => {
  it('reads the token, removes it from the address and shows the account', async () => {
    const fetchMock = stubApi(null, {
      'POST /api/osfm/reset/check': () =>
        Response.json({ valid: true, loginName: 'test_member', displayName: 'Test Member' }),
    })
    const router = renderAt(`/reset#token=${token}`)
    expect(
      await screen.findByText(
        zhCN.credentials.reset.account
          .replace('{{displayName}}', 'Test Member')
          .replace('{{loginName}}', 'test_member'),
      ),
    ).toBeInTheDocument()
    await waitFor(() => {
      expect(router.state.location.hash).toBe('')
    })
    const call = fetchMock.mock.calls.find(([p]) => p === '/api/osfm/reset/check')
    expect(body(call?.[1])).toEqual({ token })
  })

  it('has one message for an unusable link', async () => {
    stubApi(null, { 'POST /api/osfm/reset/check': () => Response.json({ valid: false }) })
    renderAt(`/reset#token=${token}`)
    expect(await screen.findByText(zhCN.credentials.reset.invalid)).toBeInTheDocument()
  })
})

/** The index-th item of a list. */
function rowOf(list: HTMLElement, index: number): HTMLElement {
  const row = within(list).getAllByRole('listitem')[index]
  if (!row) {
    throw new Error(`no row ${String(index)}`)
  }
  return row
}

describe('the admin page', () => {
  // The signed-in maintainer is `maintainer`; the member is someone else.
  const memberUuid = '0190a1b2-0000-7000-8000-000000000003'
  const memberRow = {
    uuid: memberUuid,
    displayName: 'Test Member',
    role: 'member',
    joinedAt: '2026-01-01T10:00:00Z',
    canInvite: false,
    invitedBy: { uuid: maintainer.uuid, displayName: 'Test Maintainer' },
    leftAt: null,
    passkeys: 2,
    passwordTotp: false,
    pendingResetLink: null,
  }
  const members = [
    {
      ...memberRow,
      uuid: maintainer.uuid,
      displayName: 'Test Maintainer',
      role: 'maintainer',
      canInvite: true,
      invitedBy: null,
      passkeys: 0,
      passwordTotp: true,
    },
    memberRow,
    {
      ...memberRow,
      uuid: '0190a1b2-0000-7000-8000-000000000009',
      displayName: 'Test Gone',
      leftAt: '2026-02-01T10:00:00Z',
      pendingResetLink: null,
    },
  ]
  const link = {
    uuid: '0190a1b2-0000-7000-8000-00000000b001',
    state: 'active',
    member: { uuid: memberUuid, displayName: 'Test Member' },
    createdBy: { uuid: maintainer.uuid, displayName: 'Test Maintainer' },
    createdAt: '2026-01-01T10:00:00Z',
    expiresAt: '2026-01-02T10:00:00Z',
    usedAt: null,
    revokedAt: null,
  }
  const adminApi = (handlers: Record<string, Handler> = {}) =>
    stubApi(maintainer, {
      'GET /api/osfm/me/credentials': () => Response.json(credentials()),
      'GET /api/osfm/invites': () => Response.json({ invites: [] }),
      'GET /api/osfm/members': () => Response.json({ members }),
      'GET /api/osfm/reset-links': () => Response.json({ resetLinks: [] }),
      'GET /api/osfm/instance': () => Response.json({ name: 'Test Circle', cities: [] }),
      'POST /api/osfm/reset-links': () => Response.json({ resetLink: link, token }, { status: 201 }),
      ...handlers,
    })

  it('lists the members with their sign-in methods, those who left apart', async () => {
    adminApi()
    renderAt('/admin')
    const list = await screen.findByRole('list', { name: zhCN.pages.admin.members.title })
    const rows = within(list).getAllByRole('listitem')
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent(`Test Maintainer · ${zhCN.pages.admin.members.maintainer}`)
    expect(rows[0]).toHaveTextContent(zhCN.pages.admin.members.you)
    expect(rows[0]).toHaveTextContent(zhCN.pages.admin.members.passwordTotp)
    // No reset link for oneself.
    expect(within(rowOf(list, 0)).queryByRole('button')).toBeNull()
    expect(rows[1]).toHaveTextContent(zhCN.pages.admin.members.passkeys_other.replace('{{count}}', '2'))
    expect(rows[1]).toHaveTextContent(
      zhCN.pages.admin.members.joinedInvited.replace(/.*\{\{date\}\}/, '').replace('{{name}}', 'Test Maintainer'),
    )
    expect(rows[1]).toHaveTextContent(zhCN.pages.admin.members.cannotInvite)
    const left = screen.getByRole('list', { name: zhCN.pages.admin.members.left })
    expect(within(left).getByRole('listitem')).toHaveTextContent('Test Gone')
    expect(within(left).queryByRole('button')).toBeNull()
  })

  it('creates a reset link from a member row, by uuid, and shows it once', async () => {
    const fetchMock = adminApi()
    renderAt('/admin')
    const list = await screen.findByRole('list', { name: zhCN.pages.admin.members.title })
    const row = rowOf(list, 1)
    await userEvent.click(within(row).getByRole('button', { name: zhCN.pages.admin.reset.create }))
    const shown = await screen.findByRole('textbox', { name: zhCN.pages.admin.reset.link })
    expect(shown).toHaveValue(`${window.location.origin}/reset#token=${token}`)
    const call = fetchMock.mock.calls.find(([p, init]) => p === '/api/osfm/reset-links' && init?.method === 'POST')
    expect(body(call?.[1])).toEqual({ member: memberUuid })
  })

  it('asks for a re-verification when the server says the session is stale', async () => {
    let calls = 0
    adminApi({
      'GET /api/osfm/me/credentials': () =>
        Response.json(
          credentials({ passkeys: [], passwordTotp: { createdAt: '2026-01-01T10:00:00Z', lastUsedAt: null } }),
        ),
      'POST /api/osfm/reset-links': () => {
        calls++
        return Response.json({ status: 403, message: '', data: {}, code: 'reauth_required' }, { status: 403 })
      },
    })
    renderAt('/admin')
    const list = await screen.findByRole('list', { name: zhCN.pages.admin.members.title })
    const row = rowOf(list, 1)
    await userEvent.click(within(row).getByRole('button', { name: zhCN.pages.admin.reset.create }))
    expect(await screen.findByRole('heading', { name: zhCN.credentials.verify.title })).toBeInTheDocument()
    expect(screen.getByText(zhCN.credentials.verify.repeat)).toBeInTheDocument()
    expect(calls).toBe(1)
  })
})
