import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createQueryClient } from '@/lib/queryClient'
import { sessionQueryKey } from '@/session/session'
import { clearPrivateMessage, dataCacheName, tileCacheName } from '@/sw/rules'
import { adoptPrivateCaches, cacheOwner, clearPrivateCaches, forgetPrivateData, useCacheOwner } from './privateCaches'

const precache = 'workbox-precache-v2-http://localhost/'
const alice = '0190f5a0-0000-7000-8000-00000000a11c'
const bob = '0190f5a0-0000-7000-8000-000000000b0b'

/** Cache Storage with only the names (the contents do not matter here). */
function fakeCaches(names: string[]) {
  const store = new Set(names)
  const fake = {
    keys: vi.fn(() => Promise.resolve([...store])),
    delete: vi.fn((name: string) => Promise.resolve(store.delete(name))),
  }
  vi.stubGlobal('caches', fake)
  return store
}

/** A service worker in control of the page that answers the clear message as sw.ts does. */
function fakeController(store: Set<string>) {
  const received: unknown[] = []
  const controller = {
    postMessage: vi.fn((message: { type: string }, ports: MessagePort[]) => {
      received.push(message)
      if (message.type === clearPrivateMessage) {
        for (const name of [...store]) {
          if (name.startsWith('osfm-private-')) {
            store.delete(name)
          }
        }
        ports[0]?.postMessage({ ok: true })
      }
    }),
  }
  Object.defineProperty(navigator, 'serviceWorker', { value: { controller }, configurable: true })
  return received
}

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  Reflect.deleteProperty(navigator, 'serviceWorker')
})

describe('clearPrivateCaches', () => {
  it('deletes the private caches and keeps the precache', async () => {
    const store = fakeCaches([precache, dataCacheName, tileCacheName('example-bay'), tileCacheName('other-town')])
    await clearPrivateCaches()
    expect([...store]).toEqual([precache])
  })

  it('asks the service worker first when one controls the page', async () => {
    const store = fakeCaches([precache, dataCacheName])
    const received = fakeController(store)
    await clearPrivateCaches()
    expect(received).toEqual([{ type: clearPrivateMessage }])
    expect([...store]).toEqual([precache])
  })

  it('does nothing without Cache Storage (an insecure context)', async () => {
    vi.stubGlobal('caches', undefined)
    await expect(clearPrivateCaches()).resolves.toBeUndefined()
  })
})

describe('the owner of the caches', () => {
  it('keeps the caches of the same member', async () => {
    const store = fakeCaches([dataCacheName])
    localStorage.setItem('osfm.cacheOwner', alice)
    await adoptPrivateCaches(alice)
    expect([...store]).toEqual([dataCacheName])
  })

  it("empties another member's caches before a member uses them", async () => {
    const store = fakeCaches([precache, dataCacheName, tileCacheName('example-bay')])
    localStorage.setItem('osfm.cacheOwner', alice)
    await adoptPrivateCaches(bob)
    expect([...store]).toEqual([precache])
    expect(cacheOwner()).toBe(bob)
  })

  it('empties caches of an unknown owner', async () => {
    const store = fakeCaches([dataCacheName])
    await adoptPrivateCaches(alice)
    expect([...store]).toEqual([])
    expect(cacheOwner()).toBe(alice)
  })

  it('forgets the owner with the caches', async () => {
    const store = fakeCaches([dataCacheName])
    localStorage.setItem('osfm.cacheOwner', alice)
    await forgetPrivateData()
    expect([...store]).toEqual([])
    expect(cacheOwner()).toBeNull()
  })
})

describe('useCacheOwner', () => {
  const wrapper = (client: QueryClient) =>
    function Wrapper({ children }: { children: ReactNode }) {
      return <QueryClientProvider client={client}>{children}</QueryClientProvider>
    }

  it('is ready at once for the owner', () => {
    fakeCaches([dataCacheName])
    localStorage.setItem('osfm.cacheOwner', alice)
    const { result } = renderHook(() => useCacheOwner(alice), { wrapper: wrapper(new QueryClient()) })
    expect(result.current).toBe(true)
  })

  it('is ready for another member only once the caches are empty', async () => {
    const store = fakeCaches([dataCacheName])
    localStorage.setItem('osfm.cacheOwner', alice)
    const { result } = renderHook(() => useCacheOwner(bob), { wrapper: wrapper(new QueryClient()) })
    expect(result.current).toBe(false)
    await waitFor(() => {
      expect(result.current).toBe(true)
    })
    expect([...store]).toEqual([])
    expect(cacheOwner()).toBe(bob)
  })
})

describe('the query client', () => {
  it('drops everything of the previous member when the session ends', async () => {
    const store = fakeCaches([precache, dataCacheName])
    localStorage.setItem('osfm.cacheOwner', alice)
    const client = createQueryClient()
    client.setQueryData(sessionQueryKey, { uuid: alice, displayName: 'Test Alice', role: 'member' })
    client.setQueryData(['places', 'example-bay', 'city', ''], [{ name: 'Test Place' }])
    client.setQueryData(sessionQueryKey, null)
    expect(client.getQueryData(['places', 'example-bay', 'city', ''])).toBeUndefined()
    await waitFor(() => {
      expect([...store]).toEqual([precache])
    })
    expect(cacheOwner()).toBeNull()
  })

  it('keeps the queries while the same member stays signed in', () => {
    fakeCaches([dataCacheName])
    const client = createQueryClient()
    const me = { uuid: alice, displayName: 'Test Alice', role: 'member' }
    client.setQueryData(sessionQueryKey, me)
    client.setQueryData(['places', 'example-bay', 'city', ''], [{ name: 'Test Place' }])
    client.setQueryData(sessionQueryKey, { ...me, displayName: 'Test Alice Renamed' })
    expect(client.getQueryData(['places', 'example-bay', 'city', ''])).toEqual([{ name: 'Test Place' }])
  })
})
