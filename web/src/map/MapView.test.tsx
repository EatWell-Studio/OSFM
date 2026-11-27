import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import zhCN from '@/i18n/locales/zh-CN.json'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'

const member = { uuid: '0190a1b2-0000-7000-8000-000000000001', displayName: 'Test Member', role: 'member' }

let instance: unknown = null

/** A signed-in member; GET /tiles/ answers with the given responses in turn, GET /api/osfm/instance with instance. */
function stubServer(...tiles: (() => Response)[]) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    if (input === '/api/osfm/me') {
      return Promise.resolve(Response.json(member))
    }
    if (input === '/api/osfm/instance' && instance !== null) {
      return Promise.resolve(Response.json(instance))
    }
    if (input === '/tiles/') {
      expect(init?.credentials).toBe('same-origin')
      const next = tiles.length > 1 ? tiles.shift() : tiles[0]
      return Promise.resolve(next ? next() : new Response('', { status: 404 }))
    }
    return Promise.resolve(new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderMap() {
  const queryClient = createQueryClient()
  queryClient.setDefaultOptions({ queries: { retry: false } })
  render(<App router={createMemoryRouter(routes, { initialEntries: ['/'] })} queryClient={queryClient} />)
}

afterEach(() => {
  instance = null
})

describe('the map page', () => {
  it('names the cities as the instance settings do, and says when a city has no basemap yet', async () => {
    instance = {
      name: 'Test Circle',
      cities: [{ slug: 'test-village', name: 'Test Village', bbox: [7.1, 0.2, 7.2, 0.3], center: [7.15, 0.25] }],
    }
    stubServer(() => Response.json({ cities: [] }))
    renderMap()
    expect(await screen.findByText(zhCN.map.noTiles.replace('{{city}}', 'Test Village'))).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Test Village' })).toBeInTheDocument()
    expect(screen.getByRole('banner')).toHaveTextContent('Test Circle')
  })

  it('says so when there is no city to show', async () => {
    stubServer(() => Response.json({ cities: [] }))
    renderMap()
    expect(await screen.findByText(zhCN.map.noCities)).toBeInTheDocument()
  })

  it('offers to try again when the city index cannot be loaded', async () => {
    const fetchMock = stubServer(
      () => new Response('', { status: 500 }),
      () => Response.json({ cities: [] }),
    )
    renderMap()
    expect(await screen.findByText(zhCN.map.loadError)).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('button', { name: zhCN.map.retry }))
    expect(await screen.findByText(zhCN.map.noCities)).toBeInTheDocument()
    expect(fetchMock.mock.calls.filter(([input]) => input === '/tiles/')).toHaveLength(2)
  })
})
