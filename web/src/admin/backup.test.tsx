import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import type { BackupInfo } from '@/api/types.gen'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'
import { formatSize } from './backupApi'

// Made-up values only.
const maintainer: Me = {
  uuid: '0190a1b2-0000-7000-8000-000000000001',
  displayName: 'Test Maintainer',
  role: 'maintainer',
}
const t = zhCN.backup
const full: BackupInfo = {
  name: 'osfm_20261128120000.zip',
  kind: 'osfm',
  size: 2_500_000,
  createdAt: '2026-11-28T12:00:00Z',
}
const before: BackupInfo = {
  name: 'premigrate_20261101000000.zip',
  kind: 'premigrate',
  size: 800_000,
  createdAt: '2026-11-01T00:00:00Z',
}
const created: BackupInfo = {
  name: 'osfm_20261128130000.zip',
  kind: 'osfm',
  size: 2_600_000,
  createdAt: '2026-11-28T13:00:00Z',
}

type Handler = (init?: RequestInit) => Response

function error(status: number, code: string): Response {
  return Response.json({ status, code, message: '', data: {} }, { status })
}

/** Stubs fetch: the session, the credentials (fresh or not) and the backup routes by "METHOD path". */
function stubServer(fresh: boolean, handlers: Record<string, Handler>) {
  const fetchMock = vi.fn<typeof fetch>((input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const method = init?.method ?? 'GET'
    if (url === '/api/osfm/me') {
      return Promise.resolve(Response.json(maintainer))
    }
    if (url === '/api/osfm/me/credentials') {
      return Promise.resolve(
        Response.json({
          passkeys: [],
          passwordTotp: { createdAt: '2026-01-01T00:00:00Z', lastUsedAt: null },
          recoveryCodesLeft: 10,
          verifiedUntil: fresh ? new Date(Date.now() + 600_000).toISOString() : null,
        }),
      )
    }
    const handler = handlers[`${method} ${url}`]
    return Promise.resolve(handler ? handler(init) : Response.json({}, { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function calls(fetchMock: ReturnType<typeof stubServer>, prefix: string) {
  return fetchMock.mock.calls
    .map(([input, init]) => `${init?.method ?? 'GET'} ${typeof input === 'string' ? input : 'request'}`)
    .filter((c) => c.includes(prefix))
}

function renderPage() {
  render(
    <App
      router={createMemoryRouter(routes, { initialEntries: ['/admin/backup'] })}
      queryClient={createQueryClient()}
    />,
  )
}

async function page() {
  return within(await screen.findByRole('region', { name: t.title }))
}

let clicked: { href: string; download: string }[]

beforeEach(() => {
  clicked = []
  // jsdom has neither object URLs nor downloads: record what would be saved.
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: vi.fn(() => 'blob:test'), revokeObjectURL: vi.fn() }))
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
    clicked.push({ href: this.href, download: this.download })
  })
})

describe('formatSize', () => {
  it('gives kilobytes below a megabyte and megabytes above', () => {
    expect(formatSize(800_000, 'en')).toBe('800 kB')
    expect(formatSize(2_540_000, 'en')).toBe('2.5 MB')
  })
})

describe('the backups page', () => {
  it('says the backup holds the master key, lists the backups and writes a new one', async () => {
    let list = [full, before]
    const fetchMock = stubServer(true, {
      'GET /api/osfm/backups': () => Response.json({ backups: list }),
      'POST /api/osfm/backups': () => {
        list = [created, ...list]
        return Response.json(created, { status: 201 })
      },
    })
    renderPage()
    const p = await page()
    expect(p.getByText(t.secret)).toBeInTheDocument()
    expect(p.getByText(t.restore)).toBeInTheDocument()
    const items = within(await p.findByRole('list', { name: t.list })).getAllByRole('listitem')
    expect(items.map((li) => li.getAttribute('aria-label'))).toEqual([full.name, before.name])
    expect(items[0]).toHaveTextContent(t.kind.osfm)
    expect(items[1]).toHaveTextContent(t.kind.premigrate)

    await userEvent.click(p.getByRole('button', { name: t.create }))
    expect(await p.findByRole('status')).toHaveTextContent(created.name)
    await waitFor(() => {
      expect(within(p.getByRole('list', { name: t.list })).getAllByRole('listitem')).toHaveLength(3)
    })
    const post = fetchMock.mock.calls.find(([input, init]) => input === '/api/osfm/backups' && init?.method === 'POST')
    expect(new Headers(post?.[1]?.headers).get('X-OSFM')).toBe('1')
  })

  it('asks to confirm it is you before downloading, and saves the file', async () => {
    const fetchMock = stubServer(false, {
      'GET /api/osfm/backups': () => Response.json({ backups: [full] }),
      [`GET /api/osfm/backups/${full.name}`]: () =>
        new Response('PK', { headers: { 'Content-Type': 'application/zip' } }),
    })
    renderPage()
    const p = await page()
    const item = within(await p.findByRole('listitem', { name: full.name }))
    await userEvent.click(item.getByRole('button', { name: t.download }))
    // Not verified recently: the verification comes first, nothing is fetched.
    expect(await screen.findByRole('heading', { name: zhCN.auth.credentials.verify.title })).toBeInTheDocument()
    expect(calls(fetchMock, `/backups/${full.name}`)).toEqual([])
  })

  it('downloads with a fresh session', async () => {
    const fetchMock = stubServer(true, {
      'GET /api/osfm/backups': () => Response.json({ backups: [full] }),
      [`GET /api/osfm/backups/${full.name}`]: () =>
        new Response('PK', { headers: { 'Content-Type': 'application/zip' } }),
    })
    renderPage()
    const p = await page()
    const item = within(await p.findByRole('listitem', { name: full.name }))
    await userEvent.click(item.getByRole('button', { name: t.download }))
    expect(await p.findByRole('status')).toHaveTextContent(full.name)
    expect(clicked).toEqual([{ href: 'blob:test', download: full.name }])
    expect(calls(fetchMock, `/backups/${full.name}`)).toEqual([`GET /api/osfm/backups/${full.name}`])
  })

  it('deletes a backup after confirmation', async () => {
    let list = [full, before]
    const fetchMock = stubServer(true, {
      'GET /api/osfm/backups': () => Response.json({ backups: list }),
      [`POST /api/osfm/backups/${before.name}/delete`]: () => {
        list = [full]
        return new Response(null, { status: 204 })
      },
    })
    renderPage()
    const p = await page()
    const item = within(await p.findByRole('listitem', { name: before.name }))
    await userEvent.click(item.getByRole('button', { name: t.delete }))
    expect(item.getByText(t.confirmDelete.replace('{{name}}', before.name))).toBeInTheDocument()
    expect(calls(fetchMock, '/delete')).toEqual([])
    await userEvent.click(item.getByRole('button', { name: t.deleteConfirm }))
    expect(await p.findByRole('status')).toHaveTextContent(before.name)
    await waitFor(() => {
      expect(p.queryByRole('listitem', { name: before.name })).not.toBeInTheDocument()
    })
    expect(calls(fetchMock, '/delete')).toEqual([`POST /api/osfm/backups/${before.name}/delete`])
  })

  it('explains that a backup is already being written', async () => {
    stubServer(true, {
      'GET /api/osfm/backups': () => Response.json({ backups: [] }),
      'POST /api/osfm/backups': () => error(409, 'backup_running'),
    })
    renderPage()
    const p = await page()
    expect(await p.findByText(t.empty)).toBeInTheDocument()
    await userEvent.click(p.getByRole('button', { name: t.create }))
    expect(await p.findByRole('alert')).toHaveTextContent(t.errors.running)
  })

  it('shows the verification when the server refuses a stale session', async () => {
    stubServer(true, {
      'GET /api/osfm/backups': () => Response.json({ backups: [full] }),
      'POST /api/osfm/backups': () => error(403, 'reauth_required'),
    })
    renderPage()
    const p = await page()
    await p.findByRole('listitem', { name: full.name })
    await userEvent.click(p.getByRole('button', { name: t.create }))
    expect(await screen.findByText(zhCN.auth.credentials.verify.repeat)).toBeInTheDocument()
    expect(p.queryByRole('alert')).not.toBeInTheDocument()
  })
})
