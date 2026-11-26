import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from './App'
import zhCN from './i18n/locales/zh-CN.json'
import { createQueryClient } from './lib/queryClient'
import { routes } from './routes'
import type { Me } from './session/session'

const member: Me = { uuid: '0190a1b2-0000-7000-8000-000000000001', displayName: 'Test Member', role: 'member' }

/**
 * Answers GET /api/osfm/me with the given status (and the member for 200), POST /api/osfm/logout with logoutStatus.
 * 'html' stands for a response that is not the server's JSON (something in between, such as a captive portal).
 */
function stubMe(status: number | 'html', logoutStatus = 204) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    if (input === '/api/osfm/me') {
      if (status === 'html') {
        return Promise.resolve(new Response('<!doctype html>', { headers: { 'Content-Type': 'text/html' } }))
      }
      return Promise.resolve(status === 200 ? Response.json(member) : Response.json({ message: 'x' }, { status }))
    }
    if (input === '/api/osfm/logout' && init?.method === 'POST') {
      return Promise.resolve(
        logoutStatus === 204 ? new Response(null, { status: 204 }) : Response.json({}, { status: logoutStatus }),
      )
    }
    return Promise.resolve(new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(<App router={router} queryClient={createQueryClient()} />)
  return router
}

describe('app shell', () => {
  it('shows the zh-CN bottom navigation to a signed-in member', async () => {
    stubMe(200)
    renderAt('/')
    const nav = await screen.findByRole('navigation', { name: zhCN.nav.label })
    const links = within(nav).getAllByRole('link')
    expect(links.map((link) => link.textContent)).toEqual([zhCN.nav.map, zhCN.nav.list, zhCN.nav.me])
    expect(within(nav).getByRole('link', { name: zhCN.nav.map })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(zhCN.pages.map.title)
    expect(screen.getByRole('banner')).toHaveTextContent(zhCN.app.instanceNamePlaceholder)
  })

  it('navigates between the pages', async () => {
    stubMe(200)
    const router = renderAt('/')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('link', { name: zhCN.nav.list }))
    expect(router.state.location.pathname).toBe('/list')
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(zhCN.pages.list.title)
    await user.click(screen.getByRole('link', { name: zhCN.nav.me }))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(zhCN.pages.me.title)
  })

  it.each([
    ['/admin', zhCN.pages.admin.title],
    ['/no/such/page', zhCN.pages.notFound.title],
  ])('renders %s for a signed-in member', async (path, title) => {
    stubMe(200)
    renderAt(path)
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(title)
  })
})

describe('session', () => {
  it('redirects to sign-in with next when /me answers 401', async () => {
    const fetchMock = stubMe(401)
    const router = renderAt('/list?city=x#top')
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login')
    })
    expect(new URLSearchParams(router.state.location.search).get('next')).toBe('/list?city=x')
    // The location changes before the sign-in page has rendered.
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(zhCN.pages.login.title)
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
    const init = fetchMock.mock.calls[0]?.[1]
    expect(init?.credentials).toBe('same-origin')
  })

  it.each(['/', '/me', '/admin', '/unknown'])('protects %s', async (path) => {
    stubMe(401)
    const router = renderAt(path)
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login')
    })
    expect(new URLSearchParams(router.state.location.search).get('next')).toBe(path)
  })

  it.each(['/login', '/register'])('lets %s through without a session', async (path) => {
    stubMe(401)
    const router = renderAt(path)
    expect(await screen.findByRole('heading', { level: 1 })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe(path)
  })

  it('sends a signed-in member from sign-in on to next', async () => {
    stubMe(200)
    const router = renderAt('/login?next=' + encodeURIComponent('/list?city=x'))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/list')
    })
    expect(router.state.location.search).toBe('?city=x')
  })

  it('does not follow a next that leaves the origin', async () => {
    stubMe(200)
    const router = renderAt('/login?next=' + encodeURIComponent('//evil.example/x'))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/')
    })
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(zhCN.pages.map.title)
  })

  it('offers a retry when the session cannot be checked', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockRejectedValue(new TypeError('offline')))
    renderAt('/')
    // The session query retries once (after a second) before it reports the error.
    expect(await screen.findByRole('alert', {}, { timeout: 3000 })).toHaveTextContent(zhCN.session.error)
    stubMe(200)
    await userEvent.setup().click(screen.getByRole('button', { name: zhCN.session.retry }))
    expect(await screen.findByRole('navigation', { name: zhCN.nav.label })).toBeInTheDocument()
  })

  it.each([404, 500, 'html'] as const)('treats %s from /me as an error, not as signed out', async (status) => {
    stubMe(status)
    const router = renderAt('/list')
    expect(await screen.findByRole('alert', {}, { timeout: 3000 })).toHaveTextContent(zhCN.session.error)
    expect(router.state.location.pathname).toBe('/list')
  })
})

describe('logout', () => {
  it.each([204, 401])('signs out and goes to the sign-in page when logout answers %s', async (logoutStatus) => {
    const fetchMock = stubMe(200, logoutStatus)
    const router = renderAt('/me')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: zhCN.pages.me.logout }))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login')
    })
    expect(router.state.location.search).toBe('')
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(zhCN.pages.login.title)
    const call = fetchMock.mock.calls.find(([input]) => input === '/api/osfm/logout')
    expect(call?.[1]?.method).toBe('POST')
    expect(new Headers(call?.[1]?.headers).get('X-OSFM')).toBe('1')
  })

  it('stays signed in and says so when logout fails', async () => {
    stubMe(200, 500)
    const router = renderAt('/me')
    await userEvent.setup().click(await screen.findByRole('button', { name: zhCN.pages.me.logout }))
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.pages.me.logoutError)
    expect(router.state.location.pathname).toBe('/me')
  })
})
