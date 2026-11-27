// A fake of the place search's upstream services, Photon and Nominatim, for the end-to-end tests: the global setup
// starts it and points every instance at it (OSFM_GEOCODER_PHOTON_URL, OSFM_GEOCODER_NOMINATIM_URL), so that no test
// ever asks the real public services. It answers with made-up places inside the made-up city, records the requests it
// gets, and lists them at GET /requests (for the specs: what the instance sent upstream).
//
// A search term containing failTerm makes both services fail (503); any other term finds foundPlace.

import { once } from 'node:events'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'

/** The variable through which the tests learn the fake's base URL (set by the global setup). */
export const geocoderVariable = 'OSFM_E2E_GEOCODER_URL'

/** A term with this in it makes both fake services fail. */
export const failTerm = 'test-unavailable'

/** The place every other search finds: inside the made-up city (support/env.ts), away from its center. */
export const foundPlace = { name: 'Test Harbour Market', street: 'Test Quay', lat: 0.355, lon: 7.085 } as const

/** A request the fake received. */
export interface UpstreamRequest {
  service: 'photon' | 'nominatim'
  query: Record<string, string>
  headers: Record<string, string | string[] | undefined>
}

export interface FakeGeocoder {
  /** The base URL: Photon at <url>/photon/api, Nominatim at <url>/nominatim/search. */
  url: string
  stop: () => Promise<void>
}

/** Starts the fake on a free port of 127.0.0.1. */
export async function startFakeGeocoder(): Promise<FakeGeocoder> {
  const requests: UpstreamRequest[] = []
  const server: Server = createServer((req, res) => {
    const url = new URL(req.url ?? '/', 'http://127.0.0.1')
    const json = (status: number, body: unknown) => {
      res.writeHead(status, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify(body))
    }
    if (url.pathname === '/requests') {
      json(200, requests)
      return
    }
    const service =
      url.pathname === '/photon/api' ? 'photon' : url.pathname === '/nominatim/search' ? 'nominatim' : null
    if (!service) {
      json(404, {})
      return
    }
    requests.push({ service, query: Object.fromEntries(url.searchParams), headers: req.headers })
    if ((url.searchParams.get('q') ?? '').includes(failTerm)) {
      json(503, {})
      return
    }
    if (service === 'photon') {
      json(200, {
        type: 'FeatureCollection',
        features: [
          {
            type: 'Feature',
            geometry: { type: 'Point', coordinates: [foundPlace.lon, foundPlace.lat] },
            properties: { name: foundPlace.name, street: foundPlace.street, city: 'Example Bay' },
          },
        ],
      })
    } else {
      json(200, [
        {
          lat: String(foundPlace.lat),
          lon: String(foundPlace.lon),
          name: foundPlace.name,
          display_name: foundPlace.name,
          address: { road: foundPlace.street },
        },
      ])
    }
  })
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const { port } = server.address() as AddressInfo
  return {
    url: `http://127.0.0.1:${String(port)}`,
    stop: async () => {
      server.closeAllConnections()
      server.close()
      await once(server, 'close')
    },
  }
}

/** The requests the fake received so far (from a test, through the variable the global setup set). */
export async function upstreamRequests(): Promise<UpstreamRequest[]> {
  const base = process.env[geocoderVariable]
  if (!base) {
    throw new Error(`${geocoderVariable} is not set: the global setup did not run`)
  }
  return (await (await fetch(`${base}/requests`)).json()) as UpstreamRequest[]
}

/** The environment that points an instance at the fake. */
export function geocoderEnv(fake: FakeGeocoder): Record<string, string> {
  return {
    OSFM_GEOCODER_PHOTON_URL: `${fake.url}/photon/api`,
    OSFM_GEOCODER_NOMINATIM_URL: `${fake.url}/nominatim/search`,
  }
}
