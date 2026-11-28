import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import type { ReviewItem } from '@/api/types.gen'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { ReportRecommendation } from '@/place/actions/ReportRecommendation'
import type { PlaceCard, RecommendationView } from '@/place/placeApi'
import { routes } from '@/routes'
import { type Me, sessionQueryKey } from '@/session/session'

// Made-up data only.
const t = zhCN.reports
const placeUuid = '0190a1b2-0000-7000-8000-00000000d101'
const recUuid = '0190a1b2-0000-7000-8000-00000000d201'
const member: Me = { uuid: '0190a1b2-0000-7000-8000-00000000d301', displayName: 'Test Member', role: 'member' }
const maintainer: Me = { uuid: '0190a1b2-0000-7000-8000-00000000d302', displayName: 'Test Keeper', role: 'maintainer' }

function rec(overrides: Partial<RecommendationView> = {}): RecommendationView {
  return {
    uuid: recUuid,
    displayName: 'Test Pen Name',
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

const card: PlaceCard = {
  uuid: placeUuid,
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
  recommendations: [],
}

interface Call {
  url: string
  body: unknown
}

/** Answers the routes with answer(url, body), records the POSTs. */
function stubFetch(answer: (url: string, body: unknown) => Response | undefined) {
  const calls: Call[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>((input, init) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
      const body: unknown = init?.body ? JSON.parse(init.body as string) : undefined
      if (init?.method === 'POST') {
        calls.push({ url, body })
      }
      return Promise.resolve(answer(url, body) ?? Response.json({}, { status: 404 }))
    }),
  )
  return calls
}

function renderSlot(me: Me, r: RecommendationView) {
  const queryClient = createQueryClient()
  queryClient.setQueryData(sessionQueryKey, me)
  const router = createMemoryRouter(
    [{ path: '*', element: <ReportRecommendation place={{ ...card, recommendations: [r] }} recommendation={r} /> }],
    { initialEntries: [`/p/${placeUuid}`] },
  )
  return render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  )
}

describe('reporting on the card', () => {
  it('lets a member report with a reason, a note required for "other"', async () => {
    const user = userEvent.setup()
    const calls = stubFetch((url) =>
      url === `/api/osfm/recommendations/${recUuid}/report`
        ? Response.json({ recommendation: recUuid }, { status: 201 })
        : undefined,
    )
    renderSlot(member, rec())
    expect(screen.queryByRole('button', { name: zhCN.reports.moderate.hide })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: t.report.action }))
    const form = screen.getByRole('form', { name: t.report.title })
    const submit = within(form).getByRole('button', { name: t.report.submit })
    expect(submit).toBeDisabled()
    await user.click(within(form).getByRole('radio', { name: t.reason.other }))
    expect(submit).toBeDisabled()
    await user.type(within(form).getByLabelText(t.report.noteRequired), '  Test <b>note</b>  ')
    await user.click(submit)
    expect(await screen.findByText(t.report.done)).toBeInTheDocument()
    expect(calls).toEqual([
      { url: `/api/osfm/recommendations/${recUuid}/report`, body: { reason: 'other', note: 'Test <b>note</b>' } },
    ])
  })

  it('says when the member reported it before', async () => {
    const user = userEvent.setup()
    stubFetch(() => Response.json({ code: 'already_reported', status: 409, message: '', data: {} }, { status: 409 }))
    renderSlot(member, rec())
    await user.click(screen.getByRole('button', { name: t.report.action }))
    await user.click(screen.getByRole('radio', { name: t.reason.advertising }))
    await user.click(screen.getByRole('button', { name: t.report.submit }))
    expect(await screen.findByRole('alert')).toHaveTextContent(t.report.errors.already)
  })

  it('offers no report to the author, and explains a hidden recommendation to them', () => {
    stubFetch(() => undefined)
    renderSlot(member, rec({ isMine: true, hidden: { kind: 'reports' } }))
    expect(screen.queryByRole('button', { name: t.report.action })).not.toBeInTheDocument()
    expect(screen.getByText(t.author.reports)).toBeInTheDocument()
  })

  it('lets a maintainer hide with a reason and offers no delete', async () => {
    const user = userEvent.setup()
    const calls = stubFetch((url) =>
      url === `/api/osfm/recommendations/${recUuid}/hide`
        ? Response.json(rec({ hidden: { kind: 'maintainer', reason: 'Test reason' } }))
        : undefined,
    )
    renderSlot(maintainer, rec())
    expect(screen.queryByRole('button', { name: t.report.action })).not.toBeInTheDocument()
    // No delete on the card: a violation is deleted in the review only.
    expect(screen.queryByRole('button', { name: t.queue.delete })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /delete/i })).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: t.moderate.hide }))
    const confirm = screen.getByRole('button', { name: t.moderate.confirmHide })
    expect(confirm).toBeDisabled()
    await user.type(screen.getByLabelText(t.moderate.reason), 'Test reason')
    await user.click(confirm)
    expect(await screen.findByText(t.moderate.hidden)).toBeInTheDocument()
    expect(calls).toEqual([{ url: `/api/osfm/recommendations/${recUuid}/hide`, body: { reason: 'Test reason' } }])
  })

  it('tells the maintainer when an unhidden recommendation stays hidden because its author was removed', async () => {
    const user = userEvent.setup()
    stubFetch((url) =>
      url === `/api/osfm/recommendations/${recUuid}/unhide`
        ? Response.json(rec({ hidden: { kind: 'member_removed', reason: 'Test reason' } }))
        : undefined,
    )
    renderSlot(maintainer, rec({ hidden: { kind: 'reports' } }))
    await user.click(screen.getByRole('button', { name: t.moderate.unhide }))
    await user.click(screen.getByRole('button', { name: t.moderate.confirmUnhide }))
    expect(await screen.findByText(t.moderate.stillHidden)).toBeInTheDocument()
  })
})

const reviewed: ReviewItem = {
  recommendation: rec({ hidden: { kind: 'reports' } }),
  place: { uuid: placeUuid, name: 'Test Noodle House' },
  openReports: 3,
  reasons: [
    { reason: 'abuse', count: 2 },
    { reason: 'other', count: 1 },
  ],
  notes: [{ reason: 'other', note: 'Test <i>note</i>', reportedAt: '2026-11-21T10:00:00Z' }],
  lastReportedAt: '2026-11-21T10:00:00Z',
}

function renderQueue(items: ReviewItem[], answer: (url: string, body: unknown) => Response | undefined) {
  const calls = stubFetch((url, body) => {
    if (url === '/api/osfm/me') {
      return Response.json(maintainer)
    }
    if (url === '/api/osfm/me/credentials') {
      return Response.json({
        passkeys: [],
        passwordTotp: { createdAt: '2026-01-01T00:00:00Z', lastUsedAt: null },
        recoveryCodesLeft: 10,
        verifiedUntil: new Date(Date.now() + 600_000).toISOString(),
      })
    }
    if (url === '/api/osfm/reports') {
      return Response.json({ items })
    }
    return answer(url, body)
  })
  render(
    <App
      router={createMemoryRouter(routes, { initialEntries: ['/admin/reports'] })}
      queryClient={createQueryClient()}
    />,
  )
  return calls
}

describe('the review queue', () => {
  it('shows the reports per reason and the notes as plain text, and restores', async () => {
    const user = userEvent.setup()
    const calls = renderQueue([reviewed], (url) =>
      url === `/api/osfm/reports/${recUuid}/resolve`
        ? Response.json({ resolution: 'restored', resolved: 3, recommendation: rec() })
        : undefined,
    )
    const item = await screen.findByRole('listitem', { name: 'Test Noodle House' })
    expect(within(item).getByText('Test <i>note</i>')).toBeInTheDocument()
    expect(item.querySelector('i')).toBeNull()
    expect(within(item).getByText(t.queue.openReports_other.replace('{{count}}', '3'))).toBeInTheDocument()
    expect(
      within(item).getByText(t.queue.reasonCount.replace('{{reason}}', t.reason.abuse).replace('{{count}}', '2')),
    ).toBeInTheDocument()
    await user.click(within(item).getByRole('button', { name: t.queue.restore }))
    expect(await screen.findByText(t.queue.done.restored)).toBeInTheDocument()
    expect(calls).toContainEqual({ url: `/api/osfm/reports/${recUuid}/resolve`, body: { resolution: 'restored' } })
  })

  it('deletes a violation only with a reason', async () => {
    const user = userEvent.setup()
    const calls = renderQueue([reviewed], (url) =>
      url === `/api/osfm/reports/${recUuid}/resolve`
        ? Response.json({ resolution: 'deleted', resolved: 3, recommendation: null })
        : undefined,
    )
    const item = await screen.findByRole('listitem', { name: 'Test Noodle House' })
    await user.click(within(item).getByRole('button', { name: t.queue.delete }))
    const confirm = within(item).getByRole('button', { name: t.queue.deleteConfirm })
    expect(confirm).toBeDisabled()
    await user.type(within(item).getByLabelText(t.queue.reviewReason), 'Test violation')
    await user.click(confirm)
    expect(await screen.findByText(t.queue.done.deleted)).toBeInTheDocument()
    expect(calls).toContainEqual({
      url: `/api/osfm/reports/${recUuid}/resolve`,
      body: { resolution: 'deleted', reason: 'Test violation' },
    })
  })

  it('shows the author on request', async () => {
    const user = userEvent.setup()
    renderQueue([reviewed], (url) =>
      url === `/api/osfm/reports/${recUuid}/author`
        ? Response.json({ uuid: member.uuid, displayName: 'Test Member', inCircle: true })
        : undefined,
    )
    const item = await screen.findByRole('listitem', { name: 'Test Noodle House' })
    await user.click(within(item).getByRole('button', { name: t.queue.reveal }))
    expect(await within(item).findByText(t.queue.revealed.replace('{{name}}', 'Test Member'))).toBeInTheDocument()
  })
})
