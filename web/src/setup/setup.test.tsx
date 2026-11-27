import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import zhCN from '@/i18n/locales/zh-CN.json'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'
import { normalizeSetupCode, parseBbox } from './setup'

// Made-up values only.
const setupCode = 'ABCD-EFGH-JKMN-PQRS'
const maintainer: Me = { uuid: '0190a1b2-0000-7000-8000-000000000001', displayName: 'Test First', role: 'maintainer' }
const archive = {
  id: 'example-town',
  bounds: [6.68, 0.3, 6.76, 0.36],
  center: [6.72, 0.33, 12],
  minzoom: 10,
  maxzoom: 15,
}

type Handler = (body: unknown) => Response

/**
 * Stubs fetch: /api/osfm/me answers with me() (401 for null), GET /api/osfm/setup with step() (404 for null), other
 * requests by "METHOD path" from handlers, everything else 404.
 */
function stubApi(me: () => Me | null, step: () => string | null, handlers: Record<string, Handler> = {}) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const method = init?.method ?? 'GET'
    if (path === '/api/osfm/me') {
      const m = me()
      return Promise.resolve(m ? Response.json(m) : Response.json({}, { status: 401 }))
    }
    if (path === '/api/osfm/setup' && method === 'GET') {
      const s = step()
      return Promise.resolve(s ? Response.json({ step: s }) : Response.json({ code: 'setup_done' }, { status: 404 }))
    }
    const handler = handlers[`${method} ${path}`]
    const body: unknown = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined
    return Promise.resolve(handler ? handler(body) : new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(<App router={router} queryClient={createQueryClient()} />)
  return router
}

function bodiesOf(fetchMock: ReturnType<typeof stubApi>, path: string): unknown[] {
  return fetchMock.mock.calls
    .filter(([input, init]) => input === path && init?.method === 'POST')
    .map(([, init]) => JSON.parse(init?.body as string) as unknown)
}

const s = zhCN.setup

describe('setup wizard', () => {
  it('leads every page to the wizard while there is no maintainer, and asks for the setup code', async () => {
    const fetchMock = stubApi(
      () => null,
      () => 'maintainer',
      {
        'POST /api/osfm/setup/check': (body) => Response.json({ valid: (body as { code: string }).code === setupCode }),
        'POST /api/osfm/setup/password/start': () =>
          Response.json({ code: 'login_name_taken', status: 409, message: 'x', data: {} }, { status: 409 }),
      },
    )
    const router = renderAt('/list')
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/setup')
    })
    const user = userEvent.setup()
    const field = await screen.findByLabelText(s.code.label)

    await user.type(field, 'too short')
    await user.click(screen.getByRole('button', { name: s.code.check }))
    expect(await screen.findByText(s.code.form)).toBeInTheDocument()

    await user.clear(field)
    await user.type(field, 'ZZZZ-ZZZZ-ZZZZ-ZZZZ')
    await user.click(screen.getByRole('button', { name: s.code.check }))
    expect(await screen.findByText(s.code.invalid)).toBeInTheDocument()

    await user.clear(field)
    await user.type(field, setupCode)
    await user.click(screen.getByRole('button', { name: s.code.check }))
    // Password and TOTP first and chosen, passkey second, as on the registration page.
    const methods = await screen.findAllByRole('button', { pressed: true })
    expect(methods.map((b) => b.textContent)).toEqual([zhCN.pages.register.methods.password])
    expect(screen.getByRole('button', { name: zhCN.pages.register.methods.passkey })).toHaveAttribute(
      'aria-pressed',
      'false',
    )

    const reg = zhCN.pages.register.password
    await user.type(screen.getByLabelText(reg.loginName), 'test_first')
    await user.type(screen.getByLabelText(reg.password), 'test password, made up')
    await user.type(screen.getByLabelText(reg.repeat), 'test password, made up')
    await user.click(screen.getByRole('button', { name: reg.next }))
    expect(await screen.findByText(reg.errors.loginNameTaken)).toBeInTheDocument()
    expect(bodiesOf(fetchMock, '/api/osfm/setup/password/start')).toEqual([
      { setupCode, loginName: 'test_first', displayName: '', password: 'test password, made up' },
    ])
  })

  it('asks the new maintainer for the name and cities, then opens the map', async () => {
    let done = false
    const fetchMock = stubApi(
      () => maintainer,
      () => (done ? null : 'instance'),
      {
        'GET /tiles/': () => Response.json({ cities: [archive] }),
        'POST /api/osfm/setup/complete': (body) => {
          done = true
          return Response.json(body)
        },
      },
    )
    const router = renderAt('/')
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/setup')
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: s.name.next }))
    expect(await screen.findByText(s.name.required)).toBeInTheDocument()
    await user.type(screen.getByLabelText(s.name.label), 'Test Circle')
    await user.click(screen.getByRole('button', { name: s.name.next }))

    // The archive on the server is offered under a name made from its file name, which can be changed.
    const archiveName = await screen.findByDisplayValue('Example Town')
    await user.clear(archiveName)
    await user.type(archiveName, 'Test Town')

    // A city without a basemap yet: the command that extracts it.
    await user.type(screen.getByLabelText(s.cities.add.name), 'Test Village')
    await user.type(screen.getByLabelText(s.cities.add.slug), 'test-village')
    await user.type(screen.getByLabelText(s.cities.add.bbox), '7.1, 0.2, 7.2, 0.3')
    await user.click(screen.getByRole('button', { name: s.cities.add.submit }))
    expect(await screen.findByText('deploy/tiles.sh test-village 7.1,0.2,7.2,0.3')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: s.cities.finish }))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/')
    })
    expect(bodiesOf(fetchMock, '/api/osfm/setup/complete')).toEqual([
      {
        name: 'Test Circle',
        cities: [
          { slug: 'example-town', name: 'Test Town', bbox: archive.bounds, center: [6.72, 0.33] },
          { slug: 'test-village', name: 'Test Village', bbox: [7.1, 0.2, 7.2, 0.3], center: [7.15, 0.25] },
        ],
      },
    ])
  })

  it('needs at least one city', async () => {
    stubApi(
      () => maintainer,
      () => 'instance',
      { 'GET /tiles/': () => Response.json({ cities: [] }) },
    )
    renderAt('/setup')
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText(s.name.label), 'Test Circle')
    await user.click(screen.getByRole('button', { name: s.name.next }))
    expect(await screen.findByText(s.cities.noArchives)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: s.cities.finish }))
    expect(await screen.findByText(s.cities.errors.none)).toBeInTheDocument()
  })

  it('cannot be reached once the instance is set up', async () => {
    stubApi(
      () => null,
      () => null,
    )
    const router = renderAt('/setup')
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/login')
    })
  })
})

describe('setup helpers', () => {
  it('reads setup codes as the server does', () => {
    expect(normalizeSetupCode(' abcd-efgh-jkmn-pqrs ')).toBe('ABCDEFGHJKMNPQRS')
    expect(normalizeSetupCode('ABCD EFGH JKMN PQRo')).toBe('ABCDEFGHJKMNPQR0')
    expect(normalizeSetupCode('ABCD-EFGH-JKMN-PQRU')).toBeNull()
    expect(normalizeSetupCode('ABCD-EFGH-JKMN')).toBeNull()
  })

  it('reads bounding boxes', () => {
    expect(parseBbox('6.68,0.30,6.76,0.36')).toEqual([6.68, 0.3, 6.76, 0.36])
    expect(parseBbox(' 6.68 0.30 6.76 0.36 ')).toEqual([6.68, 0.3, 6.76, 0.36])
    expect(parseBbox('6.76,0.30,6.68,0.36')).toBeNull()
    expect(parseBbox('6.68,0.30,6.76')).toBeNull()
    expect(parseBbox('170,0,190,1')).toBeNull()
    expect(parseBbox('a,b,c,d')).toBeNull()
  })
})
