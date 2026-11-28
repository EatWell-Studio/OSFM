import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import type { Notice, NoticeList } from '@/api/types.gen'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { noticeKinds, noticeLink, parseNoticeList } from '@/me/noticesApi'
import { routes } from '@/routes'
import type { Me } from '@/session/session'

// Made-up values only.
const me: Me = { uuid: '0190a1b2-0000-7000-8000-000000000002', displayName: 'Test Member', role: 'member' }
const place = '0190a1b2-0000-7000-8000-00000000e101'
const rec = '0190a1b2-0000-7000-8000-00000000e201'
const target = { recommendation: rec, place, placeName: 'Test Noodle Bar' }
const texts = zhCN.notices.kind

function notice(n: number, kind: string, withTarget: boolean, read = false): Notice {
  return {
    uuid: `0190a1b2-0000-7000-8000-00000000f${n.toString().padStart(3, '0')}`,
    kind,
    createdAt: '2026-03-01T12:00:00Z',
    read,
    target: withTarget ? target : null,
  }
}

function fill(text: string, values: Record<string, string>): string {
  return text.replace(/\{\{(\w+)\}\}/g, (_, name: string) => values[name] ?? '')
}

type Handler = (init: RequestInit | undefined) => Response | Promise<Response>

/** Stubs fetch: /api/osfm/me answers with me; other paths by "METHOD path" from handlers, 404 otherwise. */
function stubApi(handlers: Record<string, Handler> = {}) {
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

/** A server with the notices list: marking read changes it as the server does. */
function noticesServer(initial: Notice[]) {
  let notices = initial
  const list = (): NoticeList => ({ notices, unread: notices.filter((n) => !n.read).length })
  const marked: unknown[] = []
  const fetchMock = stubApi({
    'GET /api/osfm/me/notices': () => Response.json(list()),
    'POST /api/osfm/me/notices/read': (init) => {
      const body = JSON.parse(init?.body as string) as { uuids?: string[]; all?: boolean }
      marked.push(body)
      notices = notices.map((n) => (body.all || body.uuids?.includes(n.uuid) ? { ...n, read: true } : n))
      return Response.json(list())
    },
  })
  return { fetchMock, marked }
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(<App router={router} queryClient={createQueryClient()} />)
  return router
}

async function noticesSection() {
  return within(await screen.findByRole('region', { name: zhCN.notices.title }))
}

function meLink() {
  const nav = screen.getByRole('navigation', { name: zhCN.common.nav.label })
  return within(nav).getByRole('link', { name: new RegExp(`^${zhCN.common.nav.me}`) })
}

describe('noticeLink', () => {
  it('links an edit to the revisions, the rest to the card, a notice without target nowhere', () => {
    expect(noticeLink({ kind: 'maintainer_edited', target })).toBe(`/p/${place}/r/${rec}/history`)
    for (const kind of ['hidden_by_maintainer', 'hidden_by_reports', 'restored', 'violation_deleted', 'test']) {
      expect(noticeLink({ kind, target })).toBe(`/p/${place}?r=${rec}`)
      expect(noticeLink({ kind, target: null })).toBeNull()
    }
  })
})

describe('parseNoticeList', () => {
  it('takes a target only when it is complete, and refuses what is not a list', () => {
    const list = parseNoticeList({
      unread: 1,
      notices: [{ ...notice(1, 'restored', true), target: { place, placeName: 'x' } }],
    })
    expect(list.notices[0]?.target).toBeNull()
    expect(() => parseNoticeList({ notices: null, unread: 0 })).toThrow()
    expect(() => parseNoticeList({ notices: [{ kind: 'restored' }], unread: 0 })).toThrow()
  })
})

describe('the notices on the me page', () => {
  it.each(noticeKinds)(
    'shows %s with the place, linked, and without it once the recommendation is gone',
    async (kind) => {
      stubApi({
        'GET /api/osfm/me/notices': () =>
          Response.json({ notices: [notice(1, kind, true), notice(2, kind, false)], unread: 2 }),
      })
      renderAt('/me')
      const section = await noticesSection()
      const withPlace = fill(texts[kind].target, { place: target.placeName })
      const link = await section.findByRole('link', {
        name: new RegExp(withPlace.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')),
      })
      expect(link).toHaveAttribute('href', noticeLink({ kind, target }))
      const gone = section.getByText(texts[kind].gone)
      expect(gone.closest('a')).toBeNull()
      // Every text names no one: no maintainer, no reporter.
      expect(texts[kind].target).not.toMatch(/\{\{(?!place\}\})/)
      expect(texts[kind].gone).not.toMatch(/\{\{/)
    },
  )

  it('shows a generic text for a kind it does not know', async () => {
    stubApi({
      'GET /api/osfm/me/notices': () =>
        Response.json({ notices: [notice(1, 'test', true), notice(2, 'test', false)], unread: 2 }),
    })
    renderAt('/me')
    const section = await noticesSection()
    expect(await section.findByText(fill(texts.other.target, { place: target.placeName }))).toBeInTheDocument()
    expect(section.getByText(texts.other.gone)).toBeInTheDocument()
  })

  it('says when there are none, without a badge or a mark-all button', async () => {
    stubApi({ 'GET /api/osfm/me/notices': () => Response.json({ notices: [], unread: 0 }) })
    renderAt('/me')
    const section = await noticesSection()
    expect(await section.findByText(zhCN.notices.empty)).toBeInTheDocument()
    expect(section.queryByRole('button', { name: zhCN.notices.markAllRead })).toBeNull()
    expect(screen.queryByTestId('unread-notices')).toBeNull()
  })

  it('shows the unread count on "me" and lowers it as notices are read', async () => {
    const user = userEvent.setup()
    const { marked } = noticesServer([
      notice(1, 'violation_deleted', false),
      notice(2, 'hidden_by_reports', true),
      notice(3, 'restored', true),
      notice(4, 'maintainer_edited', true, true),
    ])
    renderAt('/me')
    const section = await noticesSection()
    expect(await screen.findByTestId('unread-notices')).toHaveTextContent('3')
    expect(meLink()).toHaveAccessibleName(zhCN.common.nav.me + fill(zhCN.notices.unreadBadge_other, { count: '3' }))

    // A notice whose recommendation is gone has a button.
    await user.click(section.getByRole('button', { name: zhCN.notices.markRead }))
    await waitFor(() => {
      expect(screen.getByTestId('unread-notices')).toHaveTextContent('2')
    })
    expect(marked).toEqual([{ uuids: [notice(1, '', false).uuid] }])
    expect(section.queryByRole('button', { name: zhCN.notices.markRead })).toBeNull()

    // All at once.
    await user.click(section.getByRole('button', { name: zhCN.notices.markAllRead }))
    await waitFor(() => {
      expect(screen.queryByTestId('unread-notices')).toBeNull()
    })
    expect(marked).toEqual([{ uuids: [notice(1, '', false).uuid] }, { all: true }])
    expect(section.queryByRole('button', { name: zhCN.notices.markAllRead })).toBeNull()
    expect(meLink()).toHaveAccessibleName(zhCN.common.nav.me)
  })

  it('marks a notice read when it is opened, and goes to the recommendation', async () => {
    const user = userEvent.setup()
    const { marked } = noticesServer([notice(1, 'maintainer_edited', true), notice(2, 'restored', true)])
    const router = renderAt('/me')
    const section = await noticesSection()
    await user.click(
      await section.findByRole('link', {
        name: new RegExp(fill(texts.maintainer_edited.target, { place: target.placeName })),
      }),
    )
    expect(router.state.location.pathname).toBe(`/p/${place}/r/${rec}/history`)
    await waitFor(() => {
      expect(marked).toEqual([{ uuids: [notice(1, '', false).uuid] }])
    })
    await waitFor(() => {
      expect(screen.getByTestId('unread-notices')).toHaveTextContent('1')
    })
  })

  it('shows 99+ for many unread notices, on every page', async () => {
    stubApi({ 'GET /api/osfm/me/notices': () => Response.json({ notices: [], unread: 120 }) })
    renderAt('/')
    expect(await screen.findByTestId('unread-notices')).toHaveTextContent('99+')
  })

  it('offers to try again when loading fails, without a badge', async () => {
    stubApi()
    renderAt('/me')
    const section = await noticesSection()
    expect(await section.findByText(zhCN.notices.loadError)).toBeInTheDocument()
    expect(screen.queryByTestId('unread-notices')).toBeNull()
  })
})
