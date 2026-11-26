import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import zhCN from '@/i18n/locales/zh-CN.json'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'
import { type Invite, inviteCodeFromHash, inviteLink, isInvite, isWellFormedInviteCode } from './invites'

// Made-up values only.
const code = 'dGVzdC1pbnZpdGUtY29kZS1ub3QtYS1yZWFsLW9uZSE'
const maintainer: Me = {
  uuid: '0190a1b2-0000-7000-8000-000000000001',
  displayName: 'Test Maintainer',
  role: 'maintainer',
}
const member: Me = { uuid: '0190a1b2-0000-7000-8000-000000000002', displayName: 'Test Member', role: 'member' }

function invite(overrides: Partial<Invite>): Invite {
  return {
    uuid: '0190a1b2-0000-7000-8000-00000000a001',
    state: 'active',
    createdAt: '2026-01-01T10:00:00Z',
    expiresAt: '2026-01-08T10:00:00Z',
    createdBy: { uuid: maintainer.uuid, displayName: maintainer.displayName },
    usedBy: null,
    usedAt: null,
    revokedAt: null,
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

afterEach(() => {
  sessionStorage.clear()
})

describe('invite links', () => {
  it('puts the code in the fragment and reads it back', () => {
    const link = inviteLink('https://osfm.example', code)
    expect(link).toBe(`https://osfm.example/register#invite=${code}`)
    const url = new URL(link)
    expect(url.search).toBe('')
    expect(inviteCodeFromHash(url.hash)).toBe(code)
    expect(isWellFormedInviteCode(code)).toBe(true)
  })

  it.each(['', '#', '#other=1', '#invite'])('finds no code in %j', (hash) => {
    expect(inviteCodeFromHash(hash) ?? '').toBe('')
  })

  it.each(['x', code + 'A', code.slice(1), code.slice(0, 42) + '+', code.slice(0, 42) + '='])(
    'rejects %j as a code',
    (c) => {
      expect(isWellFormedInviteCode(c)).toBe(false)
    },
  )

  it('validates invites from the server', () => {
    expect(isInvite(invite({}))).toBe(true)
    expect(isInvite({ ...invite({}), state: 'pending' })).toBe(false)
    expect(isInvite({ ...invite({}), usedBy: { uuid: 1 } })).toBe(false)
  })
})

describe('register page', () => {
  it('checks the code from the link, removes it from the address and says it is valid', async () => {
    const fetchMock = stubApi(null, { 'POST /api/osfm/invites/check': () => Response.json({ valid: true }) })
    const router = renderAt(`/register#invite=${code}`)
    expect(await screen.findByText(zhCN.pages.register.validInvite)).toHaveAttribute('role', 'status')
    expect(screen.getByText(zhCN.pages.register.formsPending)).toBeInTheDocument()
    expect(router.state.location.hash).toBe('')
    expect(router.state.location.pathname).toBe('/register')
    const call = fetchMock.mock.calls.find(([input]) => input === '/api/osfm/invites/check')
    expect(call?.[1]?.method).toBe('POST')
    expect(new Headers(call?.[1]?.headers).get('X-OSFM')).toBe('1')
    expect(JSON.parse(call?.[1]?.body as string)).toEqual({ code })
  })

  it('keeps the code for a reload of the page', async () => {
    stubApi(null, { 'POST /api/osfm/invites/check': () => Response.json({ valid: true }) })
    renderAt(`/register#invite=${code}`)
    expect(await screen.findByText(zhCN.pages.register.validInvite)).toHaveAttribute('role', 'status')
    expect(sessionStorage.getItem('osfm.inviteCode')).toBe(code)
  })

  it('says an invalid invite is invalid or expired', async () => {
    stubApi(null, { 'POST /api/osfm/invites/check': () => Response.json({ valid: false }) })
    renderAt(`/register#invite=${code}`)
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.pages.register.invalidInvite)
  })

  it('does not ask the server about a malformed code', async () => {
    const fetchMock = stubApi(null)
    renderAt('/register#invite=not-a-code')
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.pages.register.invalidInvite)
    expect(fetchMock.mock.calls.some(([input]) => input === '/api/osfm/invites/check')).toBe(false)
  })

  it('asks for an invite link without one', async () => {
    stubApi(null)
    renderAt('/register')
    expect(await screen.findByText(zhCN.pages.register.noInvite)).toBeInTheDocument()
  })
})

describe('admin page', () => {
  it('tells a member who is not a maintainer that it is not for them', async () => {
    const fetchMock = stubApi(member)
    renderAt('/admin')
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.pages.admin.notAllowed)
    expect(fetchMock.mock.calls.some(([input]) => input === '/api/osfm/invites')).toBe(false)
  })

  it('lists the invites with their state', async () => {
    stubApi(maintainer, {
      'GET /api/osfm/invites': () =>
        Response.json({
          invites: [
            invite({}),
            invite({
              uuid: '0190a1b2-0000-7000-8000-00000000a002',
              state: 'used',
              usedBy: { uuid: member.uuid, displayName: 'Test Newcomer' },
              usedAt: '2026-01-02T10:00:00Z',
            }),
            invite({ uuid: '0190a1b2-0000-7000-8000-00000000a003', state: 'expired' }),
            invite({
              uuid: '0190a1b2-0000-7000-8000-00000000a004',
              state: 'revoked',
              revokedAt: '2026-01-02T10:00:00Z',
            }),
          ],
        }),
    })
    renderAt('/admin')
    const list = await screen.findByRole('list', { name: zhCN.pages.admin.invites.title })
    const items = within(list).getAllByRole('listitem')
    expect(items.map((li) => li.querySelector('span')?.textContent)).toEqual([
      zhCN.pages.admin.invites.state.active,
      zhCN.pages.admin.invites.state.used,
      zhCN.pages.admin.invites.state.expired,
      zhCN.pages.admin.invites.state.revoked,
    ])
    expect(items[1]).toHaveTextContent('Test Newcomer')
    // Only active invites can be revoked.
    expect(screen.getAllByRole('button', { name: zhCN.pages.admin.invites.revoke })).toHaveLength(1)
  })

  it('creates an invite and shows its link once, with a copy button', async () => {
    let invites: Invite[] = []
    const fetchMock = stubApi(maintainer, {
      'GET /api/osfm/invites': () => Response.json({ invites }),
      'POST /api/osfm/invites': (init) => {
        const body = JSON.parse(init?.body as string) as { days: number }
        expect(body.days).toBe(14)
        invites = [invite({})]
        return Response.json({ invite: invites[0], code }, { status: 201 })
      },
    })
    renderAt('/admin')
    const user = userEvent.setup()
    expect(await screen.findByText(zhCN.pages.admin.invites.empty)).toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText(zhCN.pages.admin.invites.validity), '14')
    await user.click(screen.getByRole('button', { name: zhCN.pages.admin.invites.create }))

    const link = await screen.findByLabelText(zhCN.pages.admin.invites.link)
    expect(link).toHaveValue(`${window.location.origin}/register#invite=${code}`)
    expect(screen.getByText(zhCN.pages.admin.invites.shownOnce)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: zhCN.pages.admin.invites.copy }))
    // user-event provides the clipboard.
    expect(await navigator.clipboard.readText()).toBe(`${window.location.origin}/register#invite=${code}`)
    expect(await screen.findByRole('button', { name: zhCN.pages.admin.invites.copied })).toBeInTheDocument()
    const list = await screen.findByRole('list', { name: zhCN.pages.admin.invites.title })
    expect(within(list).getAllByRole('listitem')).toHaveLength(1)
    const post = fetchMock.mock.calls.find(([input, init]) => input === '/api/osfm/invites' && init?.method === 'POST')
    expect(new Headers(post?.[1]?.headers).get('X-OSFM')).toBe('1')
  })

  it('revokes an invite', async () => {
    let invites: Invite[] = [invite({})]
    const fetchMock = stubApi(maintainer, {
      'GET /api/osfm/invites': () => Response.json({ invites }),
      [`POST /api/osfm/invites/${invite({}).uuid}/revoke`]: () => {
        invites = [invite({ state: 'revoked', revokedAt: '2026-01-02T10:00:00Z' })]
        return Response.json({ invite: invites[0] })
      },
    })
    renderAt('/admin')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: zhCN.pages.admin.invites.revoke }))
    await waitFor(() => {
      expect(screen.queryByRole('button', { name: zhCN.pages.admin.invites.revoke })).not.toBeInTheDocument()
    })
    const item = within(screen.getByRole('list', { name: zhCN.pages.admin.invites.title })).getByRole('listitem')
    expect(within(item).getByText(zhCN.pages.admin.invites.state.revoked)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => typeof input === 'string' && input.endsWith('/revoke'))).toBe(true)
  })

  it('links maintainers to it from the me page', async () => {
    stubApi(maintainer)
    renderAt('/me')
    expect(await screen.findByRole('link', { name: zhCN.pages.me.admin })).toHaveAttribute('href', '/admin')
  })
})
