import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import type { InviteStatus } from '@/api/types.gen'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { daysLeft } from '@/invites/invites'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'

// Made-up values only.
const code = 'dGVzdC1pbnZpdGUtY29kZS1ub3QtYS1yZWFsLW9uZSE'
const maintainer: Me = {
  uuid: '0190a1b2-0000-7000-8000-000000000001',
  displayName: 'Test Maintainer',
  role: 'maintainer',
}
const member: Me = { uuid: '0190a1b2-0000-7000-8000-000000000002', displayName: 'Test Member', role: 'member' }
const day = 24 * 60 * 60 * 1000

type Handler = (init: RequestInit | undefined) => Response | Promise<Response>

/** Stubs fetch: /api/osfm/me answers with me; other paths by "METHOD path" from handlers, 404 otherwise. */
function stubApi(me: Me, handlers: Record<string, Handler> = {}) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (path === '/api/osfm/me') {
      return Promise.resolve(Response.json(me))
    }
    const handler = handlers[`${init?.method ?? 'GET'} ${path}`]
    return Promise.resolve(handler ? handler(init) : new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderAt(path: string) {
  render(<App router={createMemoryRouter(routes, { initialEntries: [path] })} queryClient={createQueryClient()} />)
}

function status(overrides: Partial<InviteStatus>): InviteStatus {
  return { allowed: true, reason: '', cooldownEndsAt: null, activeInvites: 0, maxActiveInvites: 3, ...overrides }
}

function fill(text: string, values: Record<string, string | number>): string {
  return text.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(values[name]))
}

async function invitesSection() {
  return within(await screen.findByRole('region', { name: zhCN.invites.title }))
}

describe('daysLeft', () => {
  it('counts a started day as a day and stops at 0', () => {
    const now = Date.parse('2026-03-01T12:00:00Z')
    expect(daysLeft('2026-03-04T12:00:00Z', now)).toBe(3)
    expect(daysLeft('2026-03-04T12:00:01Z', now)).toBe(4)
    expect(daysLeft('2026-03-01T12:00:01Z', now)).toBe(1)
    expect(daysLeft('2026-02-28T12:00:00Z', now)).toBe(0)
  })
})

describe('the invites on the me page', () => {
  it('shows a new member the days left of the cooling-off period, with the create button disabled', async () => {
    const end = new Date(Date.now() + 3.5 * day).toISOString()
    stubApi(member, {
      'GET /api/osfm/me/invite-status': () =>
        Response.json(status({ allowed: false, reason: 'cooldown', cooldownEndsAt: end })),
      'GET /api/osfm/invites': () => Response.json({ invites: [] }),
    })
    renderAt('/me')
    const section = await invitesSection()
    // The days left; the date follows in the reader's locale.
    const daysPart = fill(zhCN.invites.me.cooldown_other.split('{{date}}')[0] ?? '', { count: 4 })
    const note = await section.findByText((text) => text.startsWith(daysPart))
    expect(note).toBeInTheDocument()
    expect(section.getByRole('button', { name: zhCN.invites.create })).toBeDisabled()
  })

  it('tells a member whose permission is off', async () => {
    stubApi(member, {
      'GET /api/osfm/me/invite-status': () =>
        Response.json(status({ allowed: false, reason: 'disabled', cooldownEndsAt: '2026-01-31T10:00:00Z' })),
      'GET /api/osfm/invites': () => Response.json({ invites: [] }),
    })
    renderAt('/me')
    const section = await invitesSection()
    expect(await section.findByText(zhCN.invites.me.disabled)).toBeInTheDocument()
    expect(section.getByRole('button', { name: zhCN.invites.create })).toBeDisabled()
  })

  it('lets a member who may invite create an invite link, and says when the limit is reached', async () => {
    let active = 0
    const fetchMock = stubApi(member, {
      'GET /api/osfm/me/invite-status': () => Response.json(status({ activeInvites: active })),
      'GET /api/osfm/invites': () => Response.json({ invites: [] }),
      'POST /api/osfm/invites': () => {
        if (active >= 1) {
          return Response.json({ status: 409, code: 'invite_limit_reached', data: { limit: 3 } }, { status: 409 })
        }
        active++
        return Response.json(
          {
            invite: {
              uuid: '0190a1b2-0000-7000-8000-00000000a001',
              state: 'active',
              createdAt: '2026-03-01T10:00:00Z',
              expiresAt: '2026-03-08T10:00:00Z',
              createdBy: { uuid: member.uuid, displayName: member.displayName },
              usedBy: null,
              usedAt: null,
              revokedAt: null,
            },
            code,
          },
          { status: 201 },
        )
      },
    })
    renderAt('/me')
    const section = await invitesSection()
    expect(await section.findByText(fill(zhCN.invites.me.allowed_other, { count: 3, active: 0 }))).toBeInTheDocument()
    await userEvent.click(section.getByRole('button', { name: zhCN.invites.create }))
    expect(await section.findByLabelText(zhCN.invites.link)).toHaveValue(
      `${window.location.origin}/register#invite=${code}`,
    )
    await waitFor(() => {
      expect(section.getByText(fill(zhCN.invites.me.allowed_other, { count: 3, active: 1 }))).toBeInTheDocument()
    })
    expect(fetchMock.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)

    // The server refuses the next one (the limit, in this made-up case).
    await userEvent.click(section.getByRole('button', { name: zhCN.invites.create }))
    expect(await section.findByRole('alert')).toHaveTextContent(fill(zhCN.invites.errors.limit_other, { count: 3 }))
  })

  it('disables creating at the limit of active invites', async () => {
    stubApi(member, {
      'GET /api/osfm/me/invite-status': () => Response.json(status({ activeInvites: 3 })),
      'GET /api/osfm/invites': () => Response.json({ invites: [] }),
    })
    renderAt('/me')
    const section = await invitesSection()
    expect(await section.findByText(fill(zhCN.invites.me.limitReached_other, { count: 3 }))).toBeInTheDocument()
    expect(section.getByRole('button', { name: zhCN.invites.create })).toBeDisabled()
  })

  it('sends a maintainer to the admin page', async () => {
    stubApi(maintainer)
    renderAt('/me')
    const section = await invitesSection()
    expect(section.getByText(zhCN.invites.me.maintainer)).toBeInTheDocument()
    expect(section.getByRole('link', { name: zhCN.invites.me.toAdmin })).toHaveAttribute('href', '/admin/invites')
  })
})

describe('invite rights on the member list', () => {
  const memberRow = {
    uuid: member.uuid,
    displayName: member.displayName,
    role: 'member',
    joinedAt: new Date(Date.now() - day).toISOString(),
    canInvite: true,
    inviteCooldownEndsAt: new Date(Date.now() + 29 * day).toISOString(),
    inviteCooldown: true,
    invitedBy: { uuid: maintainer.uuid, displayName: maintainer.displayName },
    leftAt: null,
    passkeys: 1,
    passwordTotp: false,
    pendingResetLink: null,
  }

  it('switches the permission and opens inviting early', async () => {
    const rights = { uuid: member.uuid, canInvite: false, allowed: false, reason: 'disabled', cooldownEndsAt: null }
    const fetchMock = stubApi(maintainer, {
      'GET /api/osfm/members': () => Response.json({ members: [memberRow] }),
      'GET /api/osfm/me/credentials': () => Response.json({ passkeys: [], passwordTotp: null, recoveryCodesLeft: 0 }),
      'GET /api/osfm/reset-links': () => Response.json({ resetLinks: [] }),
      'POST /api/osfm/members/0190a1b2-0000-7000-8000-000000000002/invite-permission': () => Response.json(rights),
      'POST /api/osfm/members/0190a1b2-0000-7000-8000-000000000002/invite-early': () =>
        Response.json({ ...rights, canInvite: true, allowed: true, reason: '' }),
    })
    renderAt('/admin/members')
    const list = await screen.findByRole('list', { name: zhCN.members.title })
    const row = within(list).getByRole('listitem')
    const permission = within(row).getByRole('checkbox', { name: zhCN.members.invites.permission })
    expect(permission).toBeChecked()

    await userEvent.click(within(row).getByRole('button', { name: zhCN.members.invites.openEarly }))
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([path]) => (path as string).endsWith('/invite-early'))).toBe(true)
    })

    await userEvent.click(permission)
    await waitFor(() => {
      const call = fetchMock.mock.calls.find(([path]) => (path as string).endsWith('/invite-permission'))
      expect(call?.[1]?.body).toBe(JSON.stringify({ canInvite: false }))
    })
  })
})
