import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { AddRecommendation } from '@/place/actions/AddRecommendation'
import type { PlaceCard } from '@/place/placeApi'
import { routes } from '@/routes'
import type { PinView } from './PinStep'

// Made-up data only.
const member = { uuid: '0190a1b2-0000-7000-8000-000000000001', displayName: 'Test Member', role: 'member' }
const place = '0190a1b2-0000-7000-8000-00000000b001'
const created = '0190a1b2-0000-7000-8000-00000000b0aa'
const mine = '0190a1b2-0000-7000-8000-00000000c0aa'
const t = zhCN.create
const common = zhCN.common

const pin: PinView = {
  city: 'example-bay',
  center: [7.06, 0.34],
  zoom: 16,
  point: { lat: 0.34, lon: 7.06, source: 'crosshair' },
}

function card(overrides: Partial<PlaceCard> = {}): PlaceCard {
  return {
    uuid: place,
    name: 'Test Noodle House',
    aliases: [],
    lat: 0.34,
    lon: 7.06,
    precision: 'exact',
    positionNote: '',
    category: '',
    cuisine: [],
    city: 'example-bay',
    counts: { mustTry: 1, good: 0, ok: 0, avoid: 0 },
    recommendations: [
      {
        uuid: '0190a1b2-0000-7000-8000-00000000c001',
        displayName: 'Test Author',
        attitude: 'must_try',
        text: 'Test text',
        dishes: [],
        timeSlots: [],
        season: '',
        sources: [],
        createdAt: '2026-11-20T10:00:00Z',
        updatedAt: '2026-11-20T10:00:00Z',
        isMine: false,
      },
    ],
    ...overrides,
  }
}

interface Stub {
  card?: PlaceCard
  pseudonym?: string
  /** The answer to the write: status and body. */
  write?: [number, unknown]
  nearby?: unknown[]
}

/** The API of a signed-in member; records the writes. */
function stubServer(stub: Stub = {}) {
  const writes: { url: string; body: unknown }[] = []
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const url = input as string
    if (url === '/api/osfm/me') {
      return Promise.resolve(Response.json(member))
    }
    if (url === '/api/osfm/me/profile') {
      return Promise.resolve(
        Response.json({ displayName: 'Test Member', pseudonym: stub.pseudonym ?? '', externalLinks: [] }),
      )
    }
    if (url.startsWith('/api/osfm/places/nearby?')) {
      return Promise.resolve(Response.json({ places: stub.nearby ?? [] }))
    }
    if (init?.method === 'POST') {
      writes.push({ url, body: JSON.parse(init.body as string) as unknown })
      const [status, body] = stub.write ?? [201, { place: created, recommendation: mine }]
      return Promise.resolve(Response.json(body, { status }))
    }
    if (url === `/api/osfm/places/${place}` && stub.card) {
      return Promise.resolve(Response.json(stub.card))
    }
    return Promise.resolve(Response.json({ code: 'not_found' }, { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return writes
}

function renderAt(entry: string | { pathname: string; state: unknown }) {
  const router = createMemoryRouter(routes, { initialEntries: [entry] })
  const queryClient = createQueryClient()
  queryClient.setDefaultOptions({ queries: { retry: false } })
  render(<App router={router} queryClient={queryClient} />)
  return router
}

/** The second step of /new, as the crosshair leaves it. */
async function openDetails() {
  const router = renderAt({ pathname: '/new', state: { pin } })
  await screen.findByRole('heading', { level: 1, name: t.newPlace.title })
  return router
}

async function shortestPath(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText(t.place.name), 'Test Noodles')
  await user.click(screen.getByRole('radio', { name: common.attitude.good }))
  await user.type(screen.getByLabelText(t.recommendation.text), 'Test one sentence')
  await user.click(screen.getByRole('button', { name: t.newPlace.save }))
}

describe('a new place', () => {
  it('saves with only a name, an attitude and a text, and opens its card', async () => {
    const writes = stubServer()
    const user = userEvent.setup()
    const router = await openDetails()
    // No attitude is chosen at first; the circle and the main name are.
    for (const attitude of Object.values(common.attitude)) {
      expect(screen.getByRole('radio', { name: attitude })).not.toBeChecked()
    }
    expect(screen.getByRole('radio', { name: common.visibility.circle })).toBeChecked()
    await shortestPath(user)
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/p/${created}`)
    })
    expect(new URLSearchParams(router.state.location.search).get('city')).toBe('example-bay')
    expect(writes).toEqual([
      {
        url: '/api/osfm/places',
        body: {
          name: 'Test Noodles',
          lat: 0.34,
          lon: 7.06,
          sourceCoord: { crs: 'wgs84', source: 'crosshair' },
          recommendation: { attitude: 'good', text: 'Test one sentence', displayAs: 'main', visibility: 'circle' },
        },
      },
    ])
  })

  it('shows what is missing at its field and sends nothing', async () => {
    const writes = stubServer()
    const user = userEvent.setup()
    await openDetails()
    await user.click(screen.getByRole('button', { name: t.newPlace.save }))
    expect(await screen.findByText(t.errors.summary)).toBeInTheDocument()
    expect(screen.getByText(t.errors.attitude)).toBeInTheDocument()
    expect(screen.getByLabelText(t.place.name)).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText(t.recommendation.text)).toHaveAttribute('aria-invalid', 'true')
    await waitFor(() => {
      expect(screen.getByLabelText(t.place.name)).toHaveFocus()
    })
    expect(writes).toEqual([])
  })

  it('shows a field the server refuses at that field, opening its section', async () => {
    stubServer({ write: [400, { code: 'invalid_request', data: { field: 'recommendation.season' } }] })
    const user = userEvent.setup()
    await openDetails()
    expect(screen.queryByLabelText(t.recommendation.season)).not.toBeInTheDocument()
    await shortestPath(user)
    const season = await screen.findByLabelText(t.recommendation.season)
    expect(season).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText(t.errors.invalid)).toBeInTheDocument()
  })

  it('says when the point is outside the cities', async () => {
    stubServer({ write: [400, { code: 'outside_cities' }] })
    const user = userEvent.setup()
    await openDetails()
    await shortestPath(user)
    expect(await screen.findByText(t.errors.outsideCities)).toBeInTheDocument()
  })

  it('offers the place nearby with a similar name, which leads to adding a recommendation there', async () => {
    stubServer({ nearby: [{ uuid: place, name: 'Test Noodle House', category: 'restaurant', distance: 20 }] })
    const user = userEvent.setup()
    const router = await openDetails()
    await user.click(screen.getByRole('radio', { name: common.attitude.must_try }))
    await user.type(screen.getByLabelText(t.place.name), 'test noodle house')
    const hint = await screen.findByRole('region', { name: t.place.nearbyTitle })
    expect(hint).toHaveTextContent(zhCN.map.list.metres.replace('{{value}}', '20'))
    expect(hint).toHaveTextContent(common.category.restaurant)
    await user.click(within(hint).getByRole('button', { name: /Test Noodle House/ }))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/p/${place}/recommend`)
    })
    // What was typed of the recommendation comes along.
    expect((router.state.location.state as { draft: { attitude: string } }).draft.attitude).toBe('must_try')
  })

  it('offers the pseudonym only to a member who has one, and leads to "me" to set one', async () => {
    stubServer()
    await openDetails()
    const pen = await screen.findByRole('radio', { name: common.signature.pseudonym })
    expect(pen).toBeDisabled()
    expect(screen.getByRole('link', { name: t.recommendation.setPseudonym })).toHaveAttribute('href', '/me')
    expect(screen.getByRole('radio', { name: /Test Member/ })).toBeChecked()
  })

  it('needs the connection to save', async () => {
    stubServer()
    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
    await openDetails()
    expect(screen.getByText(t.errors.offline)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: t.newPlace.save })).toBeDisabled()
  })

  it('never shows coordinates', async () => {
    stubServer()
    await openDetails()
    expect(document.body.textContent).not.toMatch(/0\.34|7\.06/)
  })
})

describe('a recommendation of a place that exists', () => {
  it('saves with an attitude and a text and opens the card', async () => {
    const writes = stubServer({ card: card(), write: [201, { recommendation: mine }] })
    const user = userEvent.setup()
    const router = renderAt(`/p/${place}/recommend`)
    expect(await screen.findByText('Test Noodle House')).toBeInTheDocument()
    await user.click(screen.getByRole('radio', { name: common.attitude.avoid }))
    await user.type(screen.getByLabelText(t.recommendation.text), 'Test cold soup')
    await user.click(screen.getByRole('button', { name: t.recommend.save }))
    await waitFor(() => {
      expect(router.state.location.pathname).toBe(`/p/${place}`)
    })
    expect(writes).toEqual([
      {
        url: `/api/osfm/places/${place}/recommendations`,
        body: { attitude: 'avoid', text: 'Test cold soup', displayAs: 'main', visibility: 'circle' },
      },
    ])
  })

  it('maps the server fields without prefix', async () => {
    stubServer({ card: card(), write: [400, { code: 'invalid_request', data: { field: 'text' } }] })
    const user = userEvent.setup()
    renderAt(`/p/${place}/recommend`)
    await user.click(await screen.findByRole('radio', { name: common.attitude.ok }))
    await user.type(screen.getByLabelText(t.recommendation.text), 'Test text')
    await user.click(screen.getByRole('button', { name: t.recommend.save }))
    await waitFor(() => {
      expect(screen.getByLabelText(t.recommendation.text)).toHaveAttribute('aria-invalid', 'true')
    })
  })

  it('leads to editing the member’s own recommendation when the server says they have one', async () => {
    stubServer({ card: card(), write: [409, { code: 'already_recommended', data: { recommendation: mine } }] })
    const user = userEvent.setup()
    renderAt(`/p/${place}/recommend`)
    await user.click(await screen.findByRole('radio', { name: common.attitude.ok }))
    await user.type(screen.getByLabelText(t.recommendation.text), 'Test text')
    await user.click(screen.getByRole('button', { name: t.recommend.save }))
    expect(await screen.findByText(t.recommend.already)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: t.recommend.editMine })).toHaveAttribute(
      'href',
      `/p/${place}/r/${mine}/edit`,
    )
  })

  it('does not offer the form when the card lists the member’s own recommendation', async () => {
    const c = card()
    stubServer({ card: { ...c, recommendations: c.recommendations.map((r) => ({ ...r, uuid: mine, isMine: true })) } })
    renderAt(`/p/${place}/recommend`)
    expect(await screen.findByText(t.recommend.already)).toBeInTheDocument()
    expect(screen.queryByLabelText(t.recommendation.text)).not.toBeInTheDocument()
  })

  it('says when the place is not there', async () => {
    stubServer()
    renderAt(`/p/${place}/recommend`)
    expect(await screen.findByText(t.errors.notFound)).toBeInTheDocument()
  })
})

describe('"add my recommendation" on the card', () => {
  function renderSlot(c: PlaceCard) {
    const router = createMemoryRouter([{ path: '*', element: <AddRecommendation place={c} /> }])
    render(<RouterProvider router={router} />)
  }

  it('leads to the recommendation page', () => {
    renderSlot(card())
    expect(screen.getByRole('link', { name: t.add.button })).toHaveAttribute('href', `/p/${place}/recommend`)
  })

  it('is not there when the member has a recommendation of the place', () => {
    const c = card()
    renderSlot({ ...c, recommendations: c.recommendations.map((r) => ({ ...r, isMine: true })) })
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
})
