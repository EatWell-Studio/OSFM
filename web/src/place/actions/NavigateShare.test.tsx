import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { navigationLink } from '@/geo/navigation'
import { zhCN } from '@/i18n/resources'
import { instanceQueryKey } from '@/instance/instance'
import { recommendationElementId } from '../links'
import { PlaceCard } from '../PlaceCard'
import type { PlaceCard as Card, RecommendationView } from '../placeApi'

// Made-up data only: a place at an arbitrary point in central China.
const place = '0190a1b2-0000-7000-8000-00000000b001'
const recA = '0190a1b2-0000-7000-8000-00000000c001'
const recB = '0190a1b2-0000-7000-8000-00000000c002'
const t = zhCN.share

function rec(overrides: Partial<RecommendationView> = {}): RecommendationView {
  return {
    uuid: recA,
    displayName: 'Test Member',
    attitude: 'must_try',
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
    lat: 30.54321,
    lon: 114.31234,
    precision: 'exact',
    positionNote: '',
    category: '',
    cuisine: [],
    city: 'example-bay',
    counts: { mustTry: 1, good: 1, ok: 0, avoid: 0 },
    recommendations: [rec(), rec({ uuid: recB, displayName: 'Test Other', attitude: 'good', text: 'Test other text' })],
    ...overrides,
  }
}

function renderCard(c: Card, path = `/p/${place}`) {
  const router = createMemoryRouter([{ path: '*', element: <PlaceCard place={c} /> }], { initialEntries: [path] })
  const queryClient = new QueryClient()
  queryClient.setQueryData(instanceQueryKey, {
    name: 'Test Circle',
    cities: [{ slug: 'example-bay', name: 'Test Bay City', bbox: [114, 30, 115, 31], center: [114.5, 30.5] }],
  })
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

function element(uuid: string): HTMLElement {
  const found = document.getElementById(recommendationElementId(uuid))
  if (!found) {
    throw new Error(`no recommendation ${uuid}`)
  }
  return found
}

/** Gives the page a share sheet (jsdom has none) that records what it is given. */
function stubShare(result: () => Promise<void> = () => Promise.resolve()) {
  const share = vi.fn<(data: ShareData) => Promise<void>>(result)
  Object.defineProperty(navigator, 'share', { value: share, configurable: true })
  return share
}

afterEach(() => {
  Reflect.deleteProperty(navigator, 'share')
})

describe('directions', () => {
  it('links the place to each app, as a new window without a referrer', () => {
    renderCard(card())
    const group = screen.getByRole('group', { name: t.navigate })
    const links = within(group).getAllByRole('link')
    expect(links.map((l) => l.getAttribute('href'))).toEqual([
      navigationLink('amap', { lat: 30.54321, lon: 114.31234, name: 'Test Noodle House' }),
      navigationLink('apple', { lat: 30.54321, lon: 114.31234, name: 'Test Noodle House' }),
      navigationLink('google', { lat: 30.54321, lon: 114.31234, name: 'Test Noodle House' }),
    ])
    expect(links.map((l) => l.textContent)).toEqual([
      t.app.amap + t.newWindow,
      t.app.apple + t.newWindow,
      t.app.google + t.newWindow,
    ])
    for (const link of links) {
      expect(link).toHaveAttribute('target', '_blank')
      expect(link).toHaveAttribute('rel', 'noopener noreferrer')
      expect(link).toHaveAttribute('referrerpolicy', 'no-referrer')
    }
  })
})

describe('share with the group', () => {
  it('hands the place text with its link to the share sheet', async () => {
    const share = stubShare()
    renderCard(card())
    await userEvent.setup().click(screen.getByRole('button', { name: t.sharePlace }))
    expect(share).toHaveBeenCalledTimes(1)
    const text = share.mock.calls[0]?.[0].text ?? ''
    expect(text).toContain('Test Noodle House')
    expect(text).toContain('Test Bay City')
    expect(text.split('\n').at(-1)).toBe(`${window.location.origin}/p/${place}`)
    // The share sheet says enough; no message.
    expect(screen.queryByText(t.copied)).not.toBeInTheDocument()
  })

  it('shares one recommendation with ?r=', async () => {
    const share = stubShare()
    renderCard(card())
    const name = t.shareRecommendation.replace('{{name}}', 'Test Other')
    await userEvent.setup().click(within(element(recB)).getByRole('button', { name }))
    const text = share.mock.calls[0]?.[0].text ?? ''
    expect(text).toContain('Test other text')
    expect(text.split('\n').at(-1)).toBe(`${window.location.origin}/p/${place}?r=${recB}`)
  })

  it('copies the text when there is no share sheet, and says so', async () => {
    renderCard(card())
    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: t.sharePlace }))
    expect(await screen.findByText(t.copied)).toBeInTheDocument()
    expect(await navigator.clipboard.readText()).toMatch(new RegExp(`\\n${window.location.origin}/p/${place}$`))
  })

  it('shows the text to copy by hand when copying fails', async () => {
    renderCard(card())
    const user = userEvent.setup()
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new DOMException('no', 'NotAllowedError'))
    await user.click(screen.getByRole('button', { name: t.sharePlace }))
    const box = await screen.findByRole('textbox', { name: t.copyFailed })
    expect((box as HTMLTextAreaElement).value).toMatch(new RegExp(`/p/${place}$`))
  })

  it('offers no share on a recommendation others cannot see', () => {
    renderCard(
      card({
        recommendations: [
          rec({ isMine: true, visibility: 'private' }),
          rec({ uuid: recB, isMine: true, hidden: { kind: 'maintainer' } }),
        ],
      }),
    )
    expect(within(element(recA)).queryByRole('button', { name: /Test Member/ })).not.toBeInTheDocument()
    expect(within(element(recB)).queryByRole('button', { name: /Test Member/ })).not.toBeInTheDocument()
    // The place itself can still be shared.
    expect(screen.getByRole('button', { name: t.sharePlace })).toBeInTheDocument()
  })
})

describe('a shared link', () => {
  it('scrolls to the recommendation it names and highlights it', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    renderCard(card(), `/p/${place}?r=${recB}`)
    await waitFor(() => {
      expect(scroll).toHaveBeenCalledTimes(1)
    })
    expect(scroll.mock.contexts[0]).toBe(element(recB))
    expect(element(recB)).toHaveAttribute('data-shared', 'true')
    expect(element(recB)).toHaveAttribute('aria-current', 'true')
    expect(element(recA)).not.toHaveAttribute('data-shared')
  })

  it('opens as usual when it names no recommendation of the card', () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView')
    renderCard(card(), `/p/${place}?r=0190a1b2-0000-7000-8000-00000000c0ff`)
    expect(scroll).not.toHaveBeenCalled()
    expect(document.querySelector('[data-shared]')).toBeNull()
  })
})
