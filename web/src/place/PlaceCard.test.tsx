import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it } from 'vitest'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { sessionQueryKey } from '@/session/session'
import { recommendationElementId } from './links'
import { PlaceCard } from './PlaceCard'
import type { PlaceCard as Card, RecommendationView } from './placeApi'

// Made-up data only.
const place = '0190a1b2-0000-7000-8000-00000000b001'
const landmark = '0190a1b2-0000-7000-8000-00000000b002'
const t = zhCN.place
const common = zhCN.common

/** The text before a placeholder ("Last visit: " of "Last visit: {{date}}"), as a pattern for the start of a text. */
function before(text: string): RegExp {
  return new RegExp('^' + (text.split('{{')[0] ?? '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
}

function rec(overrides: Partial<RecommendationView> = {}): RecommendationView {
  return {
    uuid: '0190a1b2-0000-7000-8000-00000000c001',
    displayName: 'Test Member',
    attitude: 'good',
    text: 'Test text',
    dishes: [],
    timeSlots: [],
    season: '',
    sources: [],
    createdAt: '2026-11-20T10:00:00Z',
    updatedAt: '2026-11-20T10:00:00Z',
    isMine: false,
    ...overrides,
  }
}

function card(overrides: Partial<Card> = {}): Card {
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
    counts: { mustTry: 0, good: 1, ok: 0, avoid: 0 },
    recommendations: [rec()],
    ...overrides,
  }
}

function renderCard(c: Card, path = `/p/${place}`) {
  const router = createMemoryRouter([{ path: '*', element: <PlaceCard place={c} /> }], { initialEntries: [path] })
  // The action slots read the session (T036's report and hide): signed out here, so that they render nothing.
  const queryClient = createQueryClient()
  queryClient.setQueryData(sessionQueryKey, null)
  const view = render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
  return { ...view, router }
}

describe('the place card', () => {
  it('shows the place: name, aliases, category and cuisines, as plain text', () => {
    renderCard(
      card({
        name: '<b>Test</b> **House**',
        aliases: ['Test Alias One', '<i>Test</i> Alias'],
        category: 'restaurant',
        cuisine: ['sichuan', 'hot_pot'],
      }),
    )
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('<b>Test</b> **House**')
    expect(
      screen.getByText(t.aliases.replace('{{aliases}}', `Test Alias One${t.listSeparator}<i>Test</i> Alias`)),
    ).toBeInTheDocument()
    expect(screen.getByText(common.category.restaurant)).toBeInTheDocument()
    expect(screen.getByText(common.cuisine.sichuan)).toBeInTheDocument()
    expect(screen.getByText(common.cuisine.hot_pot)).toBeInTheDocument()
  })

  it('counts people per attitude, leaving out zeros, with no average', () => {
    renderCard(card({ counts: { mustTry: 2, good: 1, ok: 0, avoid: 1 } }))
    const counts = screen.getByLabelText(t.counts)
    expect(counts).toHaveTextContent(
      `${common.attitude.must_try} 2 · ${common.attitude.good} 1 · ${common.attitude.avoid} 1`,
    )
    expect(counts).not.toHaveTextContent(common.attitude.ok)
  })

  it('shows no counts when nobody counts (only hidden recommendations)', () => {
    renderCard(card({ counts: { mustTry: 0, good: 0, ok: 0, avoid: 0 } }))
    expect(screen.queryByLabelText(t.counts)).not.toBeInTheDocument()
  })

  it.each([
    ['approx', t.approximate],
    ['area', t.approximateArea],
  ])('says an %s position is approximate, without coordinates', (precision, text) => {
    const { container } = renderCard(card({ precision, lat: 0.34567, lon: 7.06789 }))
    expect(screen.getByText(text)).toBeInTheDocument()
    expect(container).not.toHaveTextContent('0.34567')
    expect(container).not.toHaveTextContent('7.06789')
  })

  it('shows no precision note and no coordinates for an exact place', () => {
    const { container } = renderCard(card({ lat: 0.34567, lon: 7.06789 }))
    expect(screen.queryByText(t.approximate)).not.toBeInTheDocument()
    expect(container).not.toHaveTextContent('0.34567')
  })

  it('keeps the position note verbatim, line breaks included, and renders no markup', () => {
    const note = 'Test line one <i>not italic</i>\n[a link](https://example.org) and **bold**'
    const { container } = renderCard(card({ positionNote: note }))
    const shown = screen.getByText((_, el) => el?.tagName === 'DD' && el.textContent === note)
    expect(shown).toHaveClass('whitespace-pre-line')
    expect(container.querySelector('i')).toBeNull()
    expect(container.querySelector('a[href="https://example.org"]')).toBeNull()
  })

  it('links a landmark place to its card, keeping the map query but not the shared recommendation', () => {
    renderCard(
      card({ landmark: { type: 'place', place: { uuid: landmark, name: 'Test Bridge Cafe' } } }),
      `/p/${place}?city=example-bay&r=0190a1b2-0000-7000-8000-00000000c001`,
    )
    expect(screen.getByRole('link', { name: 'Test Bridge Cafe' })).toHaveAttribute(
      'href',
      `/p/${landmark}?city=example-bay`,
    )
  })

  it('shows a landmark text verbatim', () => {
    renderCard(card({ landmark: { type: 'text', text: 'Test <b>gate</b>\nnorth side' } }))
    expect(screen.getByText((_, el) => el?.tagName === 'DD' && el.textContent === 'Test <b>gate</b>\nnorth side'))
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })

  it('shows the status and its last confirmation when there is one', () => {
    renderCard(card({ status: 'maybe_closed', statusConfirmedAt: '2026-10-01T08:00:00Z' }))
    expect(screen.getByText(t.status.maybe_closed, { exact: false })).toBeInTheDocument()
    expect(screen.getByText(/2026/)).toHaveTextContent(t.statusConfirmed.replace(' {{date}}', ''))
  })

  it('shows no status when nobody confirmed one', () => {
    renderCard(card())
    for (const label of Object.values(t.status)) {
      expect(screen.queryByText(label, { exact: false })).not.toBeInTheDocument()
    }
  })
})

describe('the recommendations on the card', () => {
  it('lists them in the order the server sent (newest first), each with an element id', () => {
    const first = rec({ uuid: '0190a1b2-0000-7000-8000-00000000c002', displayName: 'Test Newer', text: 'Newer' })
    const second = rec({ displayName: 'Test Older', text: 'Older' })
    renderCard(card({ recommendations: [first, second] }))
    const top = Array.from(document.querySelectorAll('li[id^="rec-"]'))
    expect(top.map((li) => li.id)).toEqual([recommendationElementId(first.uuid), recommendationElementId(second.uuid)])
    expect(top[0]).toHaveTextContent('Test Newer')
  })

  it('shows every field: signature, attitude, slots, season, dishes, text, sources, last visit, maintainer edit', () => {
    renderCard(
      card({
        recommendations: [
          rec({
            attitude: 'must_try',
            timeSlots: ['dinner', 'lunch'],
            season: 'Test winter only',
            dishes: ['Test Dish A', 'Test Dish B'],
            text: 'Test text line one\nline two',
            sources: [
              { url: 'https://example.org/test/post', kind: 'chat', excerpt: 'Test excerpt' },
              { kind: 'visited' },
            ],
            visitedOn: '2026-11-02',
            editedByMaintainerAt: '2026-11-21T09:00:00Z',
          }),
        ],
      }),
    )
    const item = document.getElementById(recommendationElementId(rec().uuid))
    if (!item) {
      throw new Error('no recommendation')
    }
    const r = within(item)
    expect(r.getByText('Test Member')).toBeInTheDocument()
    expect(r.getByText(common.attitude.must_try)).toBeInTheDocument()
    const slots = r.getByRole('list', { name: t.timeSlots })
    expect(
      within(slots)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual([common.timeSlot.lunch, common.timeSlot.dinner])
    expect(r.getByText(t.season.replace('{{season}}', 'Test winter only'))).toBeInTheDocument()
    const dishes = r.getByRole('list', { name: t.dishes })
    expect(
      within(dishes)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(['Test Dish A', 'Test Dish B'])
    expect(
      r.getByText((_, el) => el?.tagName === 'P' && el.textContent === 'Test text line one\nline two'),
    ).toHaveClass('whitespace-pre-line')
    const sources = r.getByRole('list', { name: t.sources })
    expect(within(sources).getByText(common.sourceKind.chat)).toBeInTheDocument()
    expect(within(sources).getByText(common.sourceKind.visited)).toBeInTheDocument()
    expect(within(sources).getByText('Test excerpt')).toBeInTheDocument()
    expect(r.getByText(before(t.visitedOn))).toHaveTextContent('2026')
    const edited = r.getByRole('link', { name: before(t.editedByMaintainer) })
    expect(edited).toHaveAttribute('href', `/p/${place}/r/${rec().uuid}/history`)
  })

  it('opens source links in a new window without a referrer, and links nothing else', () => {
    const { container } = renderCard(
      card({
        recommendations: [
          rec({
            sources: [
              { url: 'https://example.org/test/post?id=1', kind: 'note' },
              { url: 'javascript:alert(1)', kind: 'other' },
            ],
          }),
        ],
      }),
    )
    const links = container.querySelectorAll('a')
    expect(links).toHaveLength(1)
    const link = links[0]
    expect(link).toHaveAttribute('href', 'https://example.org/test/post?id=1')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByText('javascript:alert(1)').tagName).toBe('SPAN')
    // Nothing is fetched or prefetched: no images, no link elements.
    expect(container.querySelector('img, link, iframe')).toBeNull()
  })

  it('shows HTML and Markdown in a recommendation verbatim', () => {
    const text =
      '<script>alert(1)</script> <b>bold</b> **stars** [link](https://example.org) ![img](https://example.org/x.png)'
    const { container } = renderCard(
      card({ recommendations: [rec({ text, displayName: '<i>Test</i> _Name_', dishes: ['<u>Test</u>'] })] }),
    )
    expect(screen.getByText(text)).toBeInTheDocument()
    expect(screen.getByText('<i>Test</i> _Name_')).toBeInTheDocument()
    expect(screen.getByText('<u>Test</u>')).toBeInTheDocument()
    expect(container.querySelector('script, b, i, u, img, a')).toBeNull()
  })

  it('offers external links on a main-name signature only', () => {
    renderCard(
      card({
        recommendations: [
          rec({
            uuid: '0190a1b2-0000-7000-8000-00000000c002',
            displayName: 'Test Main Name',
            externalLinks: [{ url: 'https://example.org/test-profile' }],
          }),
          // A pseudonym recommendation: the server sends no links with it.
          rec({ displayName: 'Test Pen Name' }),
        ],
      }),
    )
    const main = document.getElementById(recommendationElementId('0190a1b2-0000-7000-8000-00000000c002'))
    const pen = document.getElementById(recommendationElementId(rec().uuid))
    if (!main || !pen) {
      throw new Error('missing recommendation')
    }
    expect(within(main).getByRole('button', { expanded: false })).toBeInTheDocument()
    expect(within(pen).queryByRole('button')).not.toBeInTheDocument()
    expect(pen.querySelector('a')).toBeNull()
  })

  it('names a member who left as such', () => {
    renderCard(card({ recommendations: [rec({ displayName: '', authorLeft: true })] }))
    expect(screen.getByText(t.authorLeft)).toBeInTheDocument()
  })

  it.each([
    ['maintainer', 'Test reason\nsecond line', t.hiddenKind.maintainer],
    ['reports', undefined, t.hiddenKind.reports],
    ['member_removed', 'Test removal reason', t.hiddenKind.member_removed],
  ])('marks a recommendation hidden (%s) with why, for its author and maintainers', (kind, reason, note) => {
    renderCard(card({ recommendations: [rec({ hidden: reason ? { kind, reason } : { kind } })] }))
    const item = document.getElementById(recommendationElementId(rec().uuid))
    if (!item) {
      throw new Error('no recommendation')
    }
    expect(item).toHaveAttribute('data-hidden', 'true')
    expect(within(item).getByText(t.hidden)).toBeInTheDocument()
    expect(within(item).getByText(note)).toBeInTheDocument()
    if (reason) {
      const why = t.hiddenReason.replace('{{reason}}', reason)
      expect(within(item).getByText((_, el) => el?.tagName === 'P' && el.textContent === why)).toHaveClass(
        'whitespace-pre-line',
      )
    } else {
      expect(within(item).queryByText(before(t.hiddenReason))).not.toBeInTheDocument()
    }
  })

  it('does not mark a recommendation that is not hidden', () => {
    renderCard(card())
    expect(screen.queryByText(t.hidden)).not.toBeInTheDocument()
  })

  it("marks the member's own private recommendation", () => {
    renderCard(card({ recommendations: [rec({ isMine: true, visibility: 'private' })] }))
    expect(screen.getByText(common.visibility.private)).toBeInTheDocument()
  })
})
