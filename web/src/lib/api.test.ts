import { describe, expect, it, vi } from 'vitest'
import { ApiError, apiFetch, apiJson, isJson } from './api'

function stubFetch(response: Response = new Response('{}', { status: 200 })) {
  const fetchMock = vi.fn<typeof fetch>().mockResolvedValue(response)
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function sent(fetchMock: ReturnType<typeof stubFetch>) {
  const call = fetchMock.mock.calls[0]
  if (!call) {
    throw new Error('fetch was not called')
  }
  const [url, init] = call
  return { url, init: init ?? {}, headers: new Headers(init?.headers) }
}

describe('apiFetch', () => {
  it.each(['POST', 'PUT', 'PATCH', 'DELETE', 'post'])('sends X-OSFM and credentials on %s', async (method) => {
    const fetchMock = stubFetch()
    await apiFetch('/places', { method, body: '{}' })
    const { url, init, headers } = sent(fetchMock)
    expect(url).toBe('/api/osfm/places')
    expect(init.method).toBe(method.toUpperCase())
    expect(init.credentials).toBe('same-origin')
    expect(headers.get('X-OSFM')).toBe('1')
  })

  it.each(['GET', 'HEAD', undefined])('sends credentials but no X-OSFM on %s', async (method) => {
    const fetchMock = stubFetch()
    await apiFetch('/me', method ? { method } : undefined)
    const { init, headers } = sent(fetchMock)
    expect(init.credentials).toBe('same-origin')
    expect(headers.has('X-OSFM')).toBe(false)
    expect(headers.get('Accept')).toBe('application/json')
  })

  it('cannot be told to omit credentials', async () => {
    const fetchMock = stubFetch()
    await apiFetch('/places', { method: 'POST', credentials: 'omit' })
    expect(sent(fetchMock).init.credentials).toBe('same-origin')
  })

  it('keeps headers given by the caller', async () => {
    const fetchMock = stubFetch()
    await apiFetch('/places', { method: 'POST', headers: { 'Content-Type': 'application/json' } })
    const { headers } = sent(fetchMock)
    expect(headers.get('Content-Type')).toBe('application/json')
    expect(headers.get('X-OSFM')).toBe('1')
  })

  it('rejects a path without a leading slash', () => {
    stubFetch()
    expect(() => apiFetch('places')).toThrow(/must start with/)
  })
})

describe('apiJson', () => {
  it('returns the parsed body', async () => {
    stubFetch(Response.json({ a: 1 }))
    await expect(apiJson('/x')).resolves.toEqual({ a: 1 })
  })

  it('rejects a successful response that is not JSON', async () => {
    stubFetch(new Response('<!doctype html>', { status: 200, headers: { 'Content-Type': 'text/html' } }))
    await expect(apiJson('/x')).rejects.toThrow(/not a JSON response/)
  })

  it('throws ApiError with the status', async () => {
    stubFetch(new Response('', { status: 401 }))
    await expect(apiJson('/me')).rejects.toEqual(new ApiError(401, '/me'))
    await expect(apiJson('/me')).rejects.toBeInstanceOf(ApiError)
  })
})

describe('isJson', () => {
  it.each([
    ['application/json', true],
    ['application/json; charset=utf-8', true],
    ['application/problem+json', true],
    ['text/html; charset=utf-8', false],
    ['application/jsonp', false],
    [null, false],
  ])('%s: %s', (type, expected) => {
    const headers = new Headers()
    if (type) {
      headers.set('Content-Type', type)
    }
    expect(isJson(new Response('', { headers }))).toBe(expected)
  })
})
