import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import { type Me, sessionQueryKey } from '@/session/session'
import { normalizeSetupCode, parseBbox } from './setup'

// Made-up values only.
const setupCode = 'ABCD-EFGH-JKMN-PQRS'
// CJK characters and a colon, escaped: the source files outside the locales hold no CJK text.
const cjkName = '\u6d4b\u8bd5\u5708: Test Circle'
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
  it('leads every page to the wizard while there is no maintainer: setup code, instance name, account', async () => {
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

    // The instance name, kept in the page: required, then sent with the account.
    await user.click(await screen.findByRole('button', { name: s.name.next }))
    expect(await screen.findByText(s.name.required)).toBeInTheDocument()
    await user.type(screen.getByLabelText(s.name.label), ` ${cjkName} `)
    await user.click(screen.getByRole('button', { name: s.name.next }))

    // Password and TOTP first and chosen, passkey second, as on the registration page.
    const methods = await screen.findAllByRole('button', { pressed: true })
    expect(methods.map((b) => b.textContent)).toEqual([zhCN.auth.register.methods.password])
    expect(screen.getByRole('button', { name: zhCN.auth.register.methods.passkey })).toHaveAttribute(
      'aria-pressed',
      'false',
    )

    const reg = zhCN.auth.register.password
    await user.type(screen.getByLabelText(reg.loginName), 'test_first')
    await user.type(screen.getByLabelText(reg.password), 'test password, made up')
    await user.type(screen.getByLabelText(reg.repeat), 'test password, made up')
    await user.click(screen.getByRole('button', { name: reg.next }))
    expect(await screen.findByText(reg.errors.loginNameTaken)).toBeInTheDocument()
    expect(bodiesOf(fetchMock, '/api/osfm/setup/password/start')).toEqual([
      {
        setupCode,
        instanceName: cjkName,
        loginName: 'test_first',
        displayName: '',
        password: 'test password, made up',
      },
    ])
  })

  it('resumes at the cities for the new maintainer, with the saved name, then opens the map', async () => {
    let done = false
    const fetchMock = stubApi(
      () => maintainer,
      () => (done ? null : 'cities'),
      {
        'GET /tiles/': () => Response.json({ cities: [archive] }),
        'GET /api/osfm/instance': () => Response.json({ name: 'Test Circle', cities: [] }),
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
    expect(await screen.findByDisplayValue('Test Circle')).toBeInTheDocument()

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
    // The name was not changed: the server keeps the saved one.
    expect(bodiesOf(fetchMock, '/api/osfm/setup/complete')).toEqual([
      {
        name: '',
        cities: [
          { slug: 'example-town', name: 'Test Town', bbox: archive.bounds, center: [6.72, 0.33] },
          { slug: 'test-village', name: 'Test Village', bbox: [7.1, 0.2, 7.2, 0.3], center: [7.15, 0.25] },
        ],
      },
    ])
  })

  it('needs at least one city, and says when there were too many attempts', async () => {
    const fetchMock = stubApi(
      () => maintainer,
      () => 'cities',
      {
        'GET /tiles/': () => Response.json({ cities: [] }),
        'POST /api/osfm/setup/complete': () =>
          Response.json(
            { code: 'rate_limited', status: 429, message: 'x', data: {} },
            { status: 429, headers: { 'Retry-After': '90' } },
          ),
      },
    )
    renderAt('/setup')
    const user = userEvent.setup()
    expect(await screen.findByText(s.cities.noArchives)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: s.cities.finish }))
    expect(await screen.findByText(s.cities.errors.none)).toBeInTheDocument()

    // A corrected name goes with the cities.
    const name = screen.getByLabelText(s.name.label)
    await user.clear(name)
    await user.type(name, 'Test Other Circle')
    await user.type(screen.getByLabelText(s.cities.add.name), 'Test Village')
    await user.type(screen.getByLabelText(s.cities.add.slug), 'test-village')
    await user.type(screen.getByLabelText(s.cities.add.bbox), '7.1,0.2,7.2,0.3')
    await user.click(screen.getByRole('button', { name: s.cities.add.submit }))
    await user.click(screen.getByRole('button', { name: s.cities.finish }))
    expect(await screen.findByText(zhCN.common.rateLimit.wait_other.replace('{{count}}', '2'))).toBeInTheDocument()
    expect(bodiesOf(fetchMock, '/api/osfm/setup/complete')).toMatchObject([{ name: 'Test Other Circle' }])
  })

  it('shows the rate limit at the setup code', async () => {
    stubApi(
      () => null,
      () => 'maintainer',
      {
        'POST /api/osfm/setup/check': () =>
          Response.json(
            { code: 'rate_limited', status: 429, message: 'x', data: {} },
            { status: 429, headers: { 'Retry-After': '60' } },
          ),
      },
    )
    renderAt('/setup')
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText(s.code.label), setupCode)
    await user.click(screen.getByRole('button', { name: s.code.check }))
    expect(await screen.findByText(zhCN.common.rateLimit.wait_one.replace('{{count}}', '1'))).toBeInTheDocument()
  })

  it('resumes at the cities when the maintainer signs in after an anonymous visit', async () => {
    let signedIn = false
    stubApi(
      () => (signedIn ? maintainer : null),
      () => (signedIn ? 'cities' : null),
      {
        'GET /tiles/': () => Response.json({ cities: [] }),
        'GET /api/osfm/instance': () => Response.json({ name: 'Test Circle', cities: [] }),
      },
    )
    const queryClient = createQueryClient()
    const router = createMemoryRouter(routes, { initialEntries: ['/login'] })
    render(<App router={router} queryClient={queryClient} />)
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(zhCN.auth.login.title)
    // Signing in, as the sign-in forms do: the session is set, and the app goes on.
    signedIn = true
    queryClient.setQueryData(sessionQueryKey, maintainer)
    await router.navigate('/')
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/setup')
    })
    expect(await screen.findByDisplayValue('Test Circle')).toBeInTheDocument()
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
