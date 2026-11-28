import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { createMemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from '@/App'
import { zhCN } from '@/i18n/resources'
import { createQueryClient } from '@/lib/queryClient'
import { routes } from '@/routes'
import type { Me } from '@/session/session'
import { exportFileName } from './personalExportApi'

// Made-up values only.
const member: Me = { uuid: '0190a1b2-0000-7000-8000-000000000002', displayName: 'Test Member', role: 'member' }
const exportBody = JSON.stringify({ 'osfm:schema': 'osfm-personal/1', account: { displayName: 'Test Member' } })
const disposition = 'attachment; filename="osfm-personal-export-2026-03-01.json"'

/** Stubs fetch: /api/osfm/me answers with member, the export with exportResponse, anything else 404. */
function stubApi(exportResponse: () => Response) {
  const fetchMock = vi.fn<typeof fetch>((input) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    if (path === '/api/osfm/me') {
      return Promise.resolve(Response.json(member))
    }
    if (path === '/api/osfm/me/export') {
      return Promise.resolve(exportResponse())
    }
    return Promise.resolve(new Response('', { status: 404 }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

async function exportSection() {
  render(<App router={createMemoryRouter(routes, { initialEntries: ['/me'] })} queryClient={createQueryClient()} />)
  return within(await screen.findByRole('region', { name: zhCN.personalExport.title }))
}

describe('exportFileName', () => {
  it("takes the server's file name, or makes one of the same form", () => {
    expect(exportFileName(disposition)).toBe('osfm-personal-export-2026-03-01.json')
    expect(exportFileName(null, new Date('2026-03-02T23:00:00Z'))).toBe('osfm-personal-export-2026-03-02.json')
    expect(exportFileName('attachment; filename="../evil name.json"', new Date('2026-03-02T00:00:00Z'))).toBe(
      'osfm-personal-export-2026-03-02.json',
    )
  })
})

describe('the personal export on the me page', () => {
  let saved: { name: string; blob: Blob }[]

  beforeEach(() => {
    saved = []
    const blobs = new Map<string, Blob>()
    // jsdom has no blob: URLs.
    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: (blob: Blob) => {
        const url = `blob:test/${String(blobs.size)}`
        blobs.set(url, blob)
        return url
      },
    })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: () => undefined })
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      const blob = blobs.get(this.href)
      if (blob) {
        saved.push({ name: this.download, blob })
      }
    })
  })

  afterEach(() => {
    Reflect.deleteProperty(URL, 'createObjectURL')
    Reflect.deleteProperty(URL, 'revokeObjectURL')
  })

  it('downloads the export as a JSON file with the name the server gives', async () => {
    const fetchMock = stubApi(
      () =>
        new Response(exportBody, {
          headers: { 'Content-Type': 'application/json; charset=utf-8', 'Content-Disposition': disposition },
        }),
    )
    const section = await exportSection()
    expect(section.getByText(zhCN.personalExport.contents.recommendations)).toBeInTheDocument()
    await userEvent.click(section.getByRole('button', { name: zhCN.personalExport.download }))

    await vi.waitFor(() => {
      expect(saved).toHaveLength(1)
    })
    expect(saved[0]?.name).toBe('osfm-personal-export-2026-03-01.json')
    expect(saved[0]?.blob.type).toBe('application/json')
    expect(JSON.parse(await (saved[0]?.blob.text() ?? Promise.resolve('null')))).toMatchObject({
      'osfm:schema': 'osfm-personal/1',
    })
    const call = fetchMock.mock.calls.find(([input]) => input === '/api/osfm/me/export')
    expect(call?.[1]?.method).toBe('GET')
    expect(call?.[1]?.credentials).toBe('same-origin')
    expect(section.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('says how long to wait when the budget is spent', async () => {
    stubApi(() => Response.json({ code: 'rate_limited' }, { status: 429, headers: { 'Retry-After': '90' } }))
    const section = await exportSection()
    await userEvent.click(section.getByRole('button', { name: zhCN.personalExport.download }))
    const alert = await section.findByRole('alert')
    expect(alert).toHaveTextContent(zhCN.common.rateLimit.wait_other.replace('{{count}}', '2'))
    expect(saved).toHaveLength(0)
  })

  it('reports a failed download and lets the member try again', async () => {
    stubApi(() => new Response('', { status: 500 }))
    const section = await exportSection()
    const button = section.getByRole('button', { name: zhCN.personalExport.download })
    await userEvent.click(button)
    expect(await section.findByRole('alert')).toHaveTextContent(zhCN.personalExport.downloadError)
    expect(button).toBeEnabled()
    expect(saved).toHaveLength(0)
  })
})
