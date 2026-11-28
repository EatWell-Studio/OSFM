import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'
import { exportFileName, layerExportPath } from './layerExportApi'

// Made-up values only.
const member: Me = { uuid: '0190a1b2-0000-7000-8000-000000000002', displayName: 'Test Member', role: 'member' }
const town = { slug: 'test-town', name: 'Test Town', bbox: [7.02, 0.31, 7.1, 0.37], center: [7.06, 0.34] }
const city = { slug: 'test-city', name: 'Test City', bbox: [20, 10, 21, 11], center: [20.5, 10.5] }
const t = zhCN.layerExport

type Handler = () => Response

/** Stubs fetch: /api/osfm/me and /api/osfm/instance answer; other paths by path from handlers, 404 otherwise. */
function stubApi(cities: unknown[], handlers: Record<string, Handler> = {}) {
  const fetchMock = vi.fn<typeof fetch>((input) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (path === '/api/osfm/me') {
      return Promise.resolve(Response.json(member))
    }
    if (path === '/api/osfm/instance') {
      return Promise.resolve(Response.json({ name: 'Test Circle', cities, geocoderEnabled: false }))
    }
    const handler = handlers[path]
    return Promise.resolve(handler ? handler() : new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function geojson(fileName: string): Response {
  return new Response('{"type":"FeatureCollection","features":[]}', {
    headers: {
      'Content-Type': 'application/geo+json',
      'Content-Disposition': `attachment; filename="${fileName}"`,
    },
  })
}

function renderMe() {
  render(<App router={createMemoryRouter(routes, { initialEntries: ['/me'] })} queryClient={createQueryClient()} />)
}

async function section() {
  return within(await screen.findByRole('region', { name: t.title }))
}

let clicked: { href: string; download: string }[]
let createObjectURL: ReturnType<typeof vi.fn>

beforeEach(() => {
  clicked = []
  createObjectURL = vi.fn(() => 'blob:test-object-url')
  // jsdom has neither object URLs nor downloads: record what would be saved.
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() }))
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    clicked.push({ href: this.href, download: this.download })
  })
})

describe('layerExportPath', () => {
  it('adds the city only when one is chosen', () => {
    expect(layerExportPath('places', '')).toBe('/exports/places')
    expect(layerExportPath('recommendations', 'test-town')).toBe('/exports/recommendations?city=test-town')
  })
})

describe('exportFileName', () => {
  it('takes a plain file name from the server and falls back otherwise', () => {
    const named = (cd: string) => new Response('', { headers: { 'Content-Disposition': cd } })
    expect(exportFileName(named('attachment; filename="osfm-places-2026-01-02.geojson"'), 'places')).toBe(
      'osfm-places-2026-01-02.geojson',
    )
    expect(exportFileName(named('attachment; filename="../evil.html"'), 'places')).toBe('osfm-places.geojson')
    expect(exportFileName(new Response(''), 'recommendations')).toBe('osfm-recommendations.geojson')
  })
})

describe('the layer export on the me page', () => {
  it('describes both layers with their licences and downloads one city', async () => {
    const fetchMock = stubApi([town, city], {
      '/api/osfm/exports/places?city=test-city': () => geojson('osfm-places-test-city-2026-01-02.geojson'),
    })
    renderMe()
    const s = await section()
    expect(s.getByText(t.places.license)).toBeInTheDocument()
    expect(s.getByText(t.recommendations.license)).toBeInTheDocument()
    expect(s.getByText(t.intro)).toBeInTheDocument()

    await userEvent.selectOptions(await s.findByLabelText(t.scope), 'test-city')
    await userEvent.click(s.getByRole('button', { name: t.places.download }))
    expect(await s.findByRole('status')).toHaveTextContent('osfm-places-test-city-2026-01-02.geojson')
    expect(clicked).toEqual([{ href: 'blob:test-object-url', download: 'osfm-places-test-city-2026-01-02.geojson' }])
    expect(createObjectURL).toHaveBeenCalledTimes(1)
    const call = fetchMock.mock.calls.find(
      ([input]) => typeof input === 'string' && input.startsWith('/api/osfm/exports/'),
    )
    expect(call?.[0]).toBe('/api/osfm/exports/places?city=test-city')
    // A GET: no X-OSFM header, the session cookie of the same origin.
    expect(call?.[1]?.method).toBe('GET')
    expect(call?.[1]?.credentials).toBe('same-origin')
  })

  it('exports the whole map without a city choice when there is one city', async () => {
    const fetchMock = stubApi([town], {
      '/api/osfm/exports/recommendations': () => geojson('osfm-recommendations-2026-01-02.geojson'),
    })
    renderMe()
    const s = await section()
    await userEvent.click(s.getByRole('button', { name: t.recommendations.download }))
    expect(await s.findByRole('status')).toHaveTextContent('osfm-recommendations-2026-01-02.geojson')
    expect(s.queryByLabelText(t.scope)).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => input === '/api/osfm/exports/recommendations')).toBe(true)
  })

  it('says how long to wait when over the budget, and saves nothing', async () => {
    stubApi([town], {
      '/api/osfm/exports/places': () =>
        Response.json(
          { status: 429, code: 'rate_limited', message: '', data: {} },
          {
            status: 429,
            headers: { 'Retry-After': '90' },
          },
        ),
    })
    renderMe()
    const s = await section()
    await userEvent.click(s.getByRole('button', { name: t.places.download }))
    expect(await s.findByRole('alert')).toHaveTextContent(t.rateLimited_other.replace('{{count}}', '2'))
    expect(clicked).toEqual([])
  })

  it('shows an error when the export fails, and the buttons work again', async () => {
    stubApi([town], { '/api/osfm/exports/recommendations': () => new Response('', { status: 500 }) })
    renderMe()
    const s = await section()
    const button = s.getByRole('button', { name: t.recommendations.download })
    await userEvent.click(button)
    expect(await s.findByRole('alert')).toHaveTextContent(t.error)
    await waitFor(() => {
      expect(button).toBeEnabled()
    })
    expect(clicked).toEqual([])
  })
})
