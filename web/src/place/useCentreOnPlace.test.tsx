import { QueryClientProvider } from '@tanstack/react-query'
import type { MapRef } from '@vis.gl/react-maplibre'
import { renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter, Route, Routes } from 'react-router'
import { describe, expect, it, vi } from 'vitest'
import type { Bounds } from '@/map/cities'
import { createQueryClient } from '@/lib/queryClient'
import { placeZoom, useCentreOnPlace } from './useCentreOnPlace'

// Made-up data only.
const place = '0190a1b2-0000-7000-8000-00000000b001'
const inCity: Bounds = [7.02, 0.31, 7.1, 0.37]
const elsewhere: Bounds = [8, 1, 8.1, 1.1]

function stubCard() {
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>(() =>
      Promise.resolve(
        Response.json({
          uuid: place,
          name: 'Test Place',
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
        }),
      ),
    ),
  )
}

function fakeMap(zoom: number) {
  const jumpTo = vi.fn()
  const ref = { current: { jumpTo, getZoom: () => zoom } as unknown as MapRef }
  return { ref, jumpTo }
}

function wrapperAt(path: string) {
  const client = createQueryClient()
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={[path]}>
          <Routes>
            <Route path="*" element={children} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>
    )
  }
}

describe('centring the map on the open place', () => {
  it('centres once per opening, at street level at least', async () => {
    stubCard()
    const { ref, jumpTo } = fakeMap(12)
    const { result, rerender } = renderHook(() => useCentreOnPlace(ref, inCity), { wrapper: wrapperAt(`/p/${place}`) })
    await waitFor(() => {
      expect(jumpTo).toHaveBeenCalledTimes(1)
    })
    expect(jumpTo).toHaveBeenCalledWith({ center: [7.06, 0.34], zoom: placeZoom })
    rerender()
    result.current()
    expect(jumpTo).toHaveBeenCalledTimes(1)
  })

  it('keeps a closer zoom', async () => {
    stubCard()
    const { ref, jumpTo } = fakeMap(17)
    renderHook(() => useCentreOnPlace(ref, inCity), { wrapper: wrapperAt(`/p/${place}`) })
    await waitFor(() => {
      expect(jumpTo).toHaveBeenCalledWith({ center: [7.06, 0.34], zoom: 17 })
    })
  })

  it('waits while the map shows another city', async () => {
    stubCard()
    const { ref, jumpTo } = fakeMap(12)
    const { rerender } = renderHook(({ bounds }) => useCentreOnPlace(ref, bounds), {
      wrapper: wrapperAt(`/p/${place}`),
      initialProps: { bounds: elsewhere },
    })
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(jumpTo).not.toHaveBeenCalled()
    // The card switched the map to the place's city.
    rerender({ bounds: inCity })
    await waitFor(() => {
      expect(jumpTo).toHaveBeenCalledTimes(1)
    })
  })

  it('does nothing on the map page without a card', async () => {
    const fetchMock = vi.fn<typeof fetch>()
    vi.stubGlobal('fetch', fetchMock)
    const { ref, jumpTo } = fakeMap(12)
    renderHook(() => useCentreOnPlace(ref, inCity), { wrapper: wrapperAt('/') })
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(jumpTo).not.toHaveBeenCalled()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
