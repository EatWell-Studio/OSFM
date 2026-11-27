import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { parseResults } from './geocodeApi'
import { PlaceSearch } from './PlaceSearch'

// Made-up values only. The instance is GET /api/osfm/instance for members; the search answers are fakes.
const t = zhCN.search
const city = { slug: 'example-bay', name: 'Example Bay', bbox: [7.02, 0.31, 7.1, 0.37], center: [7.06, 0.34] }
const results = [
  { name: 'Test Harbour Market', label: 'Test Quay 3, Example Bay', lat: 0.34, lon: 7.05 },
  { name: 'Test Lane 12', label: '', lat: 0.35, lon: 7.06 },
]

/** A server with the instance settings; searches are answered by search (default: the results above). */
function stubServer(options: { geocoderEnabled?: boolean; search?: () => Response } = {}) {
  const instance = { name: 'Test Circle', cities: [city], geocoderEnabled: options.geocoderEnabled ?? true }
  const fetchMock = vi.fn<typeof fetch>((input) => {
    const url = input as string
    if (url === '/api/osfm/instance') {
      return Promise.resolve(Response.json(instance))
    }
    if (url.startsWith('/api/osfm/geocode?')) {
      return Promise.resolve(options.search ? options.search() : Response.json(results))
    }
    return Promise.resolve(Response.json({}, { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function searches(fetchMock: ReturnType<typeof stubServer>): URL[] {
  return fetchMock.mock.calls
    .map(([input]) => input as string)
    .filter((u) => u.startsWith('/api/osfm/geocode'))
    .map((u) => new URL(u, 'https://osfm.invalid'))
}

function renderSearch(citySlug = city.slug) {
  const onSelect = vi.fn()
  render(
    <QueryClientProvider client={createQueryClient()}>
      <PlaceSearch city={citySlug} onSelect={onSelect} />
    </QueryClientProvider>,
  )
  return onSelect
}

async function searchBox() {
  return screen.findByRole('searchbox', { name: t.label })
}

describe('PlaceSearch', () => {
  it('searches on Enter only, never while typing, and hands the picked result over', async () => {
    const fetchMock = stubServer()
    const onSelect = renderSearch()
    const user = userEvent.setup()
    const box = await searchBox()
    await user.type(box, ' Test Market ')
    expect(searches(fetchMock)).toEqual([])

    await user.keyboard('{Enter}')
    const list = await screen.findByRole('list', { name: t.results })
    const sent = searches(fetchMock)
    expect(sent).toHaveLength(1)
    expect(sent[0]?.pathname).toBe('/api/osfm/geocode')
    expect(sent[0]?.searchParams.get('q')).toBe('Test Market')
    expect(sent[0]?.searchParams.get('city')).toBe('example-bay')
    expect(list).toHaveTextContent('Test Quay 3, Example Bay')

    await user.click(screen.getByRole('button', { name: /Test Harbour Market/ }))
    expect(onSelect).toHaveBeenCalledWith(results[0])
    expect(screen.queryByRole('list', { name: t.results })).not.toBeInTheDocument()
  })

  it('shows the results as text', async () => {
    stubServer({ search: () => Response.json([{ name: '<b>Test</b>', label: '**Test**', lat: 0.34, lon: 7.05 }]) })
    renderSearch()
    const user = userEvent.setup()
    await user.type(await searchBox(), 'Test')
    await user.click(screen.getByRole('button', { name: t.submit }))
    expect(await screen.findByRole('button', { name: /<b>Test<\/b>/ })).toHaveTextContent('**Test**')
  })

  it('does not search for blanks', async () => {
    const fetchMock = stubServer()
    renderSearch()
    const user = userEvent.setup()
    await user.type(await searchBox(), '   {Enter}')
    expect(searches(fetchMock)).toEqual([])
  })

  it('says when nothing was found', async () => {
    stubServer({ search: () => Response.json([]) })
    renderSearch()
    const user = userEvent.setup()
    await user.type(await searchBox(), 'Test Nowhere{Enter}')
    expect(await screen.findByText(t.noResults)).toBeInTheDocument()
  })

  it('says that the pin can be placed by hand when search is unavailable', async () => {
    stubServer({
      search: () =>
        Response.json({ status: 503, code: 'geocoder_unavailable', message: '', data: {} }, { status: 503 }),
    })
    renderSearch()
    const user = userEvent.setup()
    const box = await searchBox()
    await user.type(box, 'Test Market{Enter}')
    expect(await screen.findByText(t.unavailable)).toBeInTheDocument()
    // Editing the term clears the message; the box stays usable.
    await user.type(box, 's')
    expect(screen.queryByText(t.unavailable)).not.toBeInTheDocument()
  })

  it('treats a network failure as unavailable', async () => {
    stubServer({
      search: () => {
        throw new TypeError('Failed to fetch')
      },
    })
    renderSearch()
    const user = userEvent.setup()
    await user.type(await searchBox(), 'Test Market{Enter}')
    expect(await screen.findByText(t.unavailable)).toBeInTheDocument()
  })

  it('says how long to wait after too many searches', async () => {
    stubServer({
      search: () =>
        Response.json(
          { status: 429, code: 'rate_limited', message: '', data: {} },
          { status: 429, headers: { 'Retry-After': '90' } },
        ),
    })
    renderSearch()
    const user = userEvent.setup()
    await user.type(await searchBox(), 'Test Market{Enter}')
    expect(await screen.findByText(t.rateLimited_other.replace('{{count}}', '2'))).toBeInTheDocument()
  })

  it('is hidden when search is off', async () => {
    const fetchMock = stubServer({ geocoderEnabled: false })
    renderSearch()
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/osfm/instance', expect.anything())
    })
    await waitFor(() => {
      expect(screen.queryByRole('search')).not.toBeInTheDocument()
    })
  })

  it('hides itself when a search answers that search is off', async () => {
    stubServer({
      search: () => Response.json({ status: 404, code: 'geocoder_disabled', message: '', data: {} }, { status: 404 }),
    })
    renderSearch()
    const user = userEvent.setup()
    await user.type(await searchBox(), 'Test Market{Enter}')
    await waitFor(() => {
      expect(screen.queryByRole('search')).not.toBeInTheDocument()
    })
  })

  it('is hidden for a city the instance does not have', async () => {
    const fetchMock = stubServer()
    renderSearch('archive-only')
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/osfm/instance', expect.anything())
    })
    await waitFor(() => {
      expect(screen.queryByRole('search')).not.toBeInTheDocument()
    })
  })
})

describe('parseResults', () => {
  it('keeps the well-formed results', () => {
    expect(
      parseResults([
        results[0],
        { name: '', label: '', lat: 0.3, lon: 7 },
        { name: 'Test', label: 'x', lat: '0.3', lon: 7 },
        { name: 'Test', lat: 0.3, lon: 7 },
        null,
      ]),
    ).toEqual([results[0]])
    expect(() => parseResults({})).toThrow()
  })
})
