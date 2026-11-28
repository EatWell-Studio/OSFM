import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from './App'
import { zhCN } from './i18n/resources'
import { createQueryClient } from './lib/queryClient'
import { routes } from './routes'
import type { Me } from './session/session'

// Made-up values only.
const member: Me = { uuid: '0190a1b2-0000-7000-8000-000000000002', displayName: 'Test Member', role: 'member' }
const maintainer: Me = {
  uuid: '0190a1b2-0000-7000-8000-000000000001',
  displayName: 'Test Maintainer',
  role: 'maintainer',
}
const place = '0190a1b2-0000-7000-8000-00000000b001'
const rec = '0190a1b2-0000-7000-8000-00000000c001'

/** Stubs fetch: /api/osfm/me answers with me (401 for null); everything else 404. */
function stubApi(me: Me | null) {
  const fetchMock = vi.fn<typeof fetch>((input) => {
    if (input === '/api/osfm/me') {
      return Promise.resolve(me ? Response.json(me) : Response.json({}, { status: 401 }))
    }
    return Promise.resolve(Response.json({}, { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(<App router={router} queryClient={createQueryClient()} />)
  return router
}

// The pages of M2 that are placeholders for now, with the heading each shows.
const pages: [string, string][] = [
  [`/p/${place}/edit`, zhCN.edit.place.title],
  [`/p/${place}/r/${rec}/edit`, zhCN.edit.recommendation.title],
  [`/p/${place}/r/${rec}/history`, zhCN.edit.history.title],
]

// The pages of M2 that are built already: they need a session as well. The place card is tested in
// src/place/PlacePage.test.tsx, the new place and the recommendation pages in src/create/.
const built: [string, string][] = [
  ['/list', zhCN.map.list.title],
  ['/new', zhCN.create.newPlace.title],
  [`/p/${place}/recommend`, zhCN.create.recommend.title],
  [`/p/${place}`, zhCN.place.title],
  [`/p/${place}?r=${rec}`, zhCN.place.title],
]

describe('the paths of M2', () => {
  it.each([...pages, ...built])('%s sends a signed-out visitor to sign-in, with the way back', async (path) => {
    stubApi(null)
    const router = renderAt(path)
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login')
    })
    expect(new URLSearchParams(router.state.location.search).get('next')).toBe(path)
  })

  it.each(pages)('%s shows its page to a member', async (path, title) => {
    stubApi(member)
    renderAt(path)
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(title)
    expect(screen.getByText(zhCN.common.comingSoon)).toBeInTheDocument()
  })

  it.each([
    '/p/not-a-uuid',
    `/p/${place.toUpperCase()}`,
    `/p/${place}/r/not-a-uuid/edit`,
    '/p/0190a1b2-0000-4000-8000-00000000b001',
  ])('%s says the address is not valid', async (path) => {
    stubApi(member)
    renderAt(path)
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.common.invalidLink)
  })
})

const sections = [
  ['invites', zhCN.invites.title],
  ['members', zhCN.members.title],
  ['tree', zhCN.members.tree.title],
  ['reports', zhCN.reports.title],
  ['settings', zhCN.admin.instance.title],
  ['backup', zhCN.backup.title],
] as const

describe('the administration', () => {
  it.each(sections)('/admin/%s is not for members who are not maintainers', async (section) => {
    const fetchMock = stubApi(member)
    renderAt(`/admin/${section}`)
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.admin.notAllowed)
    expect(screen.queryByRole('navigation', { name: zhCN.admin.nav.label })).not.toBeInTheDocument()
    // No maintainer route is asked for (the bottom navigation reads the member's notices).
    expect(fetchMock.mock.calls.map(([input]) => input).sort()).toEqual(
      ['/api/osfm/me', '/api/osfm/instance', '/api/osfm/me/notices'].sort(),
    )
  })

  it.each(sections)('/admin/%s shows its section to a maintainer', async (section, heading) => {
    stubApi(maintainer)
    renderAt(`/admin/${section}`)
    expect(await screen.findByRole('heading', { level: 2, name: heading })).toBeInTheDocument()
    const nav = screen.getByRole('navigation', { name: zhCN.admin.nav.label })
    expect(within(nav).getByRole('link', { name: zhCN.admin.nav[section] })).toHaveAttribute('aria-current', 'page')
  })

  it('opens on the invites and links every section', async () => {
    stubApi(maintainer)
    const router = renderAt('/admin')
    const nav = await screen.findByRole('navigation', { name: zhCN.admin.nav.label })
    expect(router.state.location.pathname).toBe('/admin/invites')
    const user = userEvent.setup()
    for (const [section] of sections) {
      await user.click(within(nav).getByRole('link', { name: zhCN.admin.nav[section] }))
      expect(router.state.location.pathname).toBe(`/admin/${section}`)
    }
  })
})

describe('the me page', () => {
  it('has a section for each part, placeholders included', async () => {
    stubApi(member)
    renderAt('/me')
    for (const title of [
      zhCN.notices.title,
      zhCN.profile.title,
      zhCN.invites.title,
      zhCN.personalExport.title,
      zhCN.layerExport.title,
      zhCN.leave.title,
    ]) {
      expect(await screen.findByRole('heading', { level: 2, name: title })).toBeInTheDocument()
    }
    expect(screen.getByRole('button', { name: zhCN.me.logout })).toBeInTheDocument()
  })
})
