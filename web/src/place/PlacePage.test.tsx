import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { PlaceCard } from './placeApi'

// Made-up data only.
const member = { uuid: '0190a1b2-0000-7000-8000-000000000001', displayName: 'Test Member', role: 'member' }
const place = '0190a1b2-0000-7000-8000-00000000b001'
const other = '0190a1b2-0000-7000-8000-00000000b002'
const missing = '0190a1b2-0000-7000-8000-00000000b0ff'
const rec = '0190a1b2-0000-7000-8000-00000000c001'
const t = zhCN.place

function card(uuid: string, name: string, overrides: Partial<PlaceCard> = {}): PlaceCard {
  return {
    uuid,
    name,
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
        uuid: rec,
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

const cards: Record<string, PlaceCard> = {
  [place]: card(place, 'Test Noodle House', {
    landmark: { type: 'place', place: { uuid: other, name: 'Test Bridge Cafe' } },
  }),
  [other]: card(other, 'Test Bridge Cafe'),
}

/** A signed-in member; the cards above, 404 for any other place; the map has no city index (it is not under test). */
function stubServer() {
  const fetchMock = vi.fn<typeof fetch>((input) => {
    const url = input as string
    if (url === '/api/osfm/me') {
      return Promise.resolve(Response.json(member))
    }
    const match = /^\/api\/osfm\/places\/([0-9a-f-]+)$/.exec(url)
    if (match?.[1]) {
      const found = cards[match[1]]
      return Promise.resolve(found ? Response.json(found) : Response.json({ code: 'not_found' }, { status: 404 }))
    }
    return Promise.resolve(Response.json({}, { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  const queryClient = createQueryClient()
  queryClient.setDefaultOptions({ queries: { retry: false } })
  render(<App router={router} queryClient={queryClient} />)
  return router
}

describe('the place card page', () => {
  it('opens the card on the map page and switches the map to the place city', async () => {
    stubServer()
    const router = renderAt(`/p/${place}`)
    const heading = await screen.findByRole('heading', { level: 2, name: 'Test Noodle House' })
    // The map page is there too: its heading, and the card as a region named by the place.
    expect(screen.getByRole('heading', { level: 1, name: zhCN.map.title })).toBeInTheDocument()
    expect(screen.getByRole('complementary', { name: 'Test Noodle House' })).toBeInTheDocument()
    await waitFor(() => {
      expect(heading).toHaveFocus()
    })
    await waitFor(() => {
      expect(new URLSearchParams(router.state.location.search).get('city')).toBe('example-bay')
    })
    expect(router.state.location.pathname).toBe(`/p/${place}`)
  })

  it('says a place that is missing or not visible cannot be found', async () => {
    const fetchMock = stubServer()
    renderAt(`/p/${missing}`)
    expect(await screen.findByRole('heading', { level: 2, name: t.notFound })).toBeInTheDocument()
    // Asked once: a 404 is final.
    expect(fetchMock.mock.calls.filter(([input]) => (input as string).includes(missing))).toHaveLength(1)
  })

  it('jumps to a landmark place, keeping the map query', async () => {
    stubServer()
    const router = renderAt(`/p/${place}?city=example-bay&r=${rec}`)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('link', { name: 'Test Bridge Cafe' }))
    expect(await screen.findByRole('heading', { level: 2, name: 'Test Bridge Cafe' })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe(`/p/${other}`)
    expect(router.state.location.search).toBe('?city=example-bay')
  })

  it('closes back to the map with the same query, by the button or Escape', async () => {
    stubServer()
    const router = renderAt(`/p/${place}?city=example-bay&r=${rec}`)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: t.close }))
    expect(router.state.location.pathname).toBe('/')
    expect(router.state.location.search).toBe('?city=example-bay')
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()

    await router.navigate(`/p/${other}`)
    await screen.findByRole('heading', { level: 2, name: 'Test Bridge Cafe' })
    await user.keyboard('{Escape}')
    expect(router.state.location.pathname).toBe('/')
  })

  it('says the address is not valid for a place that is not a UUID', async () => {
    const fetchMock = stubServer()
    renderAt('/p/not-a-place')
    expect(await screen.findByRole('alert')).toHaveTextContent(zhCN.common.invalidLink)
    expect(fetchMock.mock.calls.some(([input]) => (input as string).startsWith('/api/osfm/places'))).toBe(false)
  })
})
