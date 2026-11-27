import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'
import { parseCircleSettings } from './circleSettingsApi'

// Made-up values only. The fields are those of GET/POST /api/osfm/instance for maintainers.
const maintainer: Me = {
  uuid: '0190a1b2-0000-7000-8000-000000000001',
  displayName: 'Test Maintainer',
  role: 'maintainer',
}
const city = { slug: 'example-bay', name: 'Example Bay', bbox: [7.02, 0.31, 7.1, 0.37], center: [7.06, 0.34] }
const t = zhCN.admin.circle

type Body = Record<string, unknown>

/** A server holding the instance settings; POST /api/osfm/instance replaces them with the body (or answers status). */
function stubServer(initial: Body, post?: (body: Body) => Response) {
  let instance = initial
  const posted: Body[] = []
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const method = init?.method ?? 'GET'
    if (input === '/api/osfm/me') {
      return Promise.resolve(Response.json(maintainer))
    }
    if (input === '/api/osfm/instance' && method === 'GET') {
      return Promise.resolve(Response.json(instance))
    }
    if (input === '/api/osfm/instance' && method === 'POST') {
      const body = JSON.parse(init?.body as string) as Body
      posted.push(body)
      const answer = post?.(body)
      if (answer) {
        return Promise.resolve(answer)
      }
      instance = body
      return Promise.resolve(Response.json(instance))
    }
    return Promise.resolve(Response.json({}, { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return { fetchMock, posted }
}

const saved = {
  name: 'Test Circle',
  cities: [city],
  reportThreshold: 3,
  inviteCooldownDays: 7,
  geocoderEnabled: true,
  geocoderContact: '',
}

function renderSettings() {
  const router = createMemoryRouter(routes, { initialEntries: ['/admin/settings'] })
  render(<App router={router} queryClient={createQueryClient()} />)
}

describe('parseCircleSettings', () => {
  it('reads the fields, and null without them', () => {
    expect(parseCircleSettings(saved)).toEqual({
      reportThreshold: 3,
      inviteCooldownDays: 7,
      geocoderEnabled: true,
      geocoderContact: '',
    })
    expect(parseCircleSettings({ name: 'Test Circle', cities: [] })).toBeNull()
    expect(parseCircleSettings({ ...saved, reportThreshold: 0 })).toBeNull()
    expect(parseCircleSettings({ ...saved, inviteCooldownDays: 1.5 })).toBeNull()
  })
})

describe('the instance settings', () => {
  it('shows the saved settings and saves changes with the name and cities', async () => {
    const { posted, fetchMock } = stubServer(saved)
    renderSettings()
    const user = userEvent.setup()
    const threshold = await screen.findByLabelText(t.reportThreshold)
    expect(threshold).toHaveValue('3')
    expect(screen.getByLabelText(t.inviteCooldownDays)).toHaveValue('7')
    expect(screen.getByRole('checkbox', { name: t.geocoderEnabled })).toBeChecked()

    await user.clear(threshold)
    await user.type(threshold, '2')
    await user.clear(screen.getByLabelText(t.inviteCooldownDays))
    await user.type(screen.getByLabelText(t.inviteCooldownDays), '0')
    await user.click(screen.getByRole('checkbox', { name: t.geocoderEnabled }))
    await user.type(screen.getByLabelText(t.geocoderContact), ' admin@example.org ')
    await user.click(screen.getByRole('button', { name: t.save }))

    expect(await screen.findByText(t.saved)).toBeInTheDocument()
    expect(posted).toEqual([
      {
        name: 'Test Circle',
        cities: [city],
        reportThreshold: 2,
        inviteCooldownDays: 0,
        geocoderEnabled: false,
        geocoderContact: 'admin@example.org',
      },
    ])
    const post = fetchMock.mock.calls.find(([, init]) => init?.method === 'POST')
    expect(new Headers(post?.[1]?.headers).get('X-OSFM')).toBe('1')
  })

  it('checks the numbers before sending anything', async () => {
    const { posted } = stubServer(saved)
    renderSettings()
    const user = userEvent.setup()
    const threshold = await screen.findByLabelText(t.reportThreshold)
    await user.clear(threshold)
    await user.type(threshold, '0')
    await user.clear(screen.getByLabelText(t.inviteCooldownDays))
    await user.type(screen.getByLabelText(t.inviteCooldownDays), '-1')
    await user.type(screen.getByLabelText(t.geocoderContact), 'x'.repeat(201))
    await user.click(screen.getByRole('button', { name: t.save }))
    expect(await screen.findByText(t.errors.reportThreshold)).toBeInTheDocument()
    expect(screen.getByText(t.errors.inviteCooldownDays)).toBeInTheDocument()
    expect(screen.getByText(t.errors.geocoderContact)).toBeInTheDocument()
    expect(posted).toEqual([])
  })

  it('says when the server refuses the settings', async () => {
    stubServer(saved, () => Response.json({ code: 'invalid_request', message: 'x' }, { status: 400 }))
    renderSettings()
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: t.save }))
    expect(await screen.findByText(t.errors.invalid)).toBeInTheDocument()
  })

  it('shows a note instead of the form on a server without these settings', async () => {
    stubServer({ name: 'Test Circle', cities: [city] })
    renderSettings()
    expect(await screen.findByText(t.unavailable)).toBeInTheDocument()
    expect(screen.queryByLabelText(t.reportThreshold)).not.toBeInTheDocument()
    // The name and cities are still there.
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: zhCN.admin.instance.title })).toBeInTheDocument()
    })
  })
})
