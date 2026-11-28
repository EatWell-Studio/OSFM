import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'

const member = { uuid: '0190a1b2-0000-7000-8000-000000000001', displayName: 'Test Member', role: 'member' }
const instance = {
  name: 'Test Circle',
  cities: [{ slug: 'test-bay', name: 'Test Bay', bbox: [7.02, 0.31, 7.1, 0.37], center: [7.06, 0.34] }],
}
const counts = (mustTry: number, good: number, ok: number, avoid: number) => ({ mustTry, good, ok, avoid })
const places = [
  {
    uuid: '0190a1b2-0000-7000-8000-00000000b001',
    name: 'Test Far Noodles',
    lat: 0.34,
    lon: 7.08,
    precision: 'exact',
    category: 'restaurant',
    counts: counts(2, 1, 0, 1),
    timeSlots: ['lunch'],
  },
  {
    uuid: '0190a1b2-0000-7000-8000-00000000b002',
    name: '<b>Test Near Cafe</b>',
    lat: 0.3401,
    lon: 7.0601,
    precision: 'approx',
    category: 'cafe',
    counts: counts(0, 1, 0, 0),
    timeSlots: [],
  },
]

function stubServer() {
  const fetchMock = vi.fn<typeof fetch>((input) => {
    const url = typeof input === 'string' ? input : ''
    if (url === '/api/osfm/me') {
      return Promise.resolve(Response.json(member))
    }
    if (url === '/api/osfm/instance') {
      return Promise.resolve(Response.json(instance))
    }
    if (url.startsWith('/api/osfm/places?')) {
      return Promise.resolve(Response.json(places))
    }
    if (url.startsWith('/api/osfm/signatures?')) {
      return Promise.resolve(Response.json(['Test Signer']))
    }
    return Promise.resolve(new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderList(path: string) {
  const queryClient = createQueryClient()
  queryClient.setDefaultOptions({ queries: { retry: false } })
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  render(<App router={router} queryClient={queryClient} />)
  return router
}

const t = zhCN.map

describe('the list', () => {
  it('lists the places of the city by distance with the filters of the address', async () => {
    const fetchMock = stubServer()
    renderList('/list?slots=lunch&mustTry=1')
    const list = await screen.findByRole('list', { name: t.list.places })
    const rows = within(list).getAllByRole('link')
    // The nearest first; names are plain text.
    expect(rows.map((r) => r.querySelector('.font-medium')?.textContent)).toEqual([
      '<b>Test Near Cafe</b>',
      'Test Far Noodles',
    ])
    expect(rows[0]).toHaveTextContent(`${zhCN.common.category.cafe}${zhCN.common.attitude.good} 1`)
    expect(rows[0]).toHaveTextContent(t.list.metres.replace('{{value}}', '20'))
    expect(rows[1]).toHaveTextContent(
      `${zhCN.common.attitude.must_try} 2 · ${zhCN.common.attitude.good} 1 · ${zhCN.common.attitude.avoid} 1`,
    )
    expect(rows[1]).toHaveTextContent(t.list.kilometres.replace('{{value}}', '2.2'))
    // Each row opens the card, keeping the filters.
    expect(rows[1]).toHaveAttribute('href', `/p/${places[0]?.uuid ?? ''}?slots=lunch&mustTry=1`)

    const request = fetchMock.mock.calls
      .map(([input]) => (typeof input === 'string' ? input : ''))
      .find((u) => u.startsWith('/api/osfm/places?'))
    const params = new URL(request ?? '', 'http://localhost').searchParams
    expect(params.get('city')).toBe('test-bay')
    expect(params.get('slots')).toBe('lunch')
    expect(params.get('mustTry')).toBe('1')
    // No view of the map yet: the whole city.
    expect(params.has('bbox')).toBe(false)
    expect(screen.getByText(t.list.fromCityCenter.replace('{{city}}', 'Test Bay'))).toBeInTheDocument()

    // The navigation keeps the filters for the map.
    const nav = screen.getByRole('navigation', { name: zhCN.common.nav.label })
    expect(within(nav).getByRole('link', { name: zhCN.common.nav.map })).toHaveAttribute(
      'href',
      '/?slots=lunch&mustTry=1',
    )
  })

  it('changes the filters in the address from the sheet, and shows how many are chosen', async () => {
    stubServer()
    const router = renderList('/list?city=test-bay&slots=lunch')
    const user = userEvent.setup()
    const button = await screen.findByRole('button', { name: t.filters.buttonCount_one.replace('{{count}}', '1') })
    await user.click(button)
    const dialog = await screen.findByRole('dialog', { name: t.filters.title })
    const slots = within(dialog).getByRole('group', { name: t.filters.slots })
    expect(within(slots).getByRole('button', { name: zhCN.common.timeSlot.lunch })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
    await user.click(within(slots).getByRole('button', { name: zhCN.common.timeSlot.dinner }))
    await user.click(within(dialog).getByRole('checkbox', { name: t.filters.noAvoid }))
    const signers = within(dialog).getByRole('group', { name: t.filters.by })
    await user.click(await within(signers).findByRole('button', { name: 'Test Signer' }))
    expect(router.state.location.search).toBe('?city=test-bay&slots=lunch%2Cdinner&by=Test+Signer&noAvoid=1')
    expect(
      screen.getByRole('button', { name: t.filters.buttonCount_other.replace('{{count}}', '4'), hidden: true }),
    ).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: t.filters.clear }))
    expect(router.state.location.search).toBe('?city=test-bay')
  })
})
