// An instance of its own for the measurements of the map with many places (map.e2e.ts, pwa.e2e.ts): 1000 made-up
// places, made through the API by 20 made-up members. Started by the test that needs it, stopped at its end.

import { mkdtempSync, rmSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import type { TestInfo } from '@playwright/test'
import type { CreatePlaceRequest } from '../../src/api/types.gen.ts'
import { city } from './env.ts'
import { geocoderVariable } from './geocoder.ts'
import { startServer } from './server.ts'
import { ApiClient, registerWithPassword, setUpSharedInstance, type TestMember } from './shared.ts'

const [minLon, minLat, maxLon, maxLat] = city.archive.bbox

/** A free TCP port on 127.0.0.1. */
function freePort(): Promise<string> {
  return new Promise((done, fail) => {
    const server = createServer()
    server.on('error', fail)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      server.close(() => {
        if (address && typeof address === 'object') {
          done(String(address.port))
        } else {
          fail(new Error('no port'))
        }
      })
    })
  })
}

/** A small deterministic random generator (the same places every run). */
function random(seed: number): () => number {
  let state = seed
  return () => {
    state = (state * 1_103_515_245 + 12_345) % 2_147_483_648
    return state / 2_147_483_648
  }
}

export const perfPlaces = 1000
const perfMembers = 20

/** The instance of the measurements, with its maintainer's session, its members and its data directory. */
export interface PerfInstance {
  baseURL: string
  /** The maintainer's session cookie. */
  session: string
  members: TestMember[]
  dataDir: string
  stop: () => Promise<void>
}

/**
 * An instance of its own for a measurement, set up as the shared one is, with 1000 made-up places spread over the
 * city by 20 members (each within the per-member write budget). Its own, so that the other specs' maps stay small.
 * logName names the server log in the test's output directory.
 */
export async function startPerfInstance(testInfo: TestInfo, logName = 'osfm-server-perf.log'): Promise<PerfInstance> {
  const port = await freePort()
  const dir = mkdtempSync(join(tmpdir(), 'osfm-e2e-perf-'))
  const bin = resolve(
    process.env.OSFM_E2E_BINARY ?? join(import.meta.dirname, '..', '..', 'node_modules', '.cache', 'osfm-e2e', 'osfm'),
  )
  const geocoder = process.env[geocoderVariable] ?? 'http://127.0.0.1:1'
  const server = await startServer(bin, port, join(dir, 'data'), testInfo.outputPath(logName), {
    OSFM_GEOCODER_PHOTON_URL: `${geocoder}/photon/api`,
    OSFM_GEOCODER_NOMINATIM_URL: `${geocoder}/nominatim/search`,
  })
  const stop = async () => {
    await server.stop()
    rmSync(dir, { recursive: true, force: true })
  }
  try {
    const state = await setUpSharedInstance(port, join(dir, 'data'), server.setupCode)
    const maintainer = new ApiClient(port, { session: state.maintainer.session })
    const next = random(28)
    const pick = <T>(list: readonly T[]): T => list[Math.floor(next() * list.length)] as T
    const inset = 0.002
    let made = 0
    const members: TestMember[] = []
    for (let m = 0; m < perfMembers; m++) {
      const invite = (await maintainer.post('/invites', { days: 1 })) as { code: string }
      const member = await registerWithPassword(
        new ApiClient(port),
        { start: '/register/password/start', confirm: '/register/password/confirm' },
        { invite: invite.code },
        {
          loginName: `test_perf_${String(m)}`,
          displayName: `Test Perf ${String(m)}`,
          password: 'test password, made up',
        },
      )
      members.push(member)
      const client = new ApiClient(port, { session: member.session })
      const count = perfPlaces / perfMembers
      const bodies: CreatePlaceRequest[] = []
      for (let i = 0; i < count; i++) {
        const r = next()
        bodies.push({
          name: `Test Perf Place ${String(made + i).padStart(4, '0')}`,
          lat: minLat + inset + next() * (maxLat - minLat - 2 * inset),
          lon: minLon + inset + next() * (maxLon - minLon - 2 * inset),
          precision: r < 0.7 ? 'exact' : r < 0.9 ? 'approx' : 'area',
          category: pick(['restaurant', 'fast_food', 'cafe', 'bakery', 'street_vendor', 'bar'] as const),
          recommendation: {
            attitude: pick(['must_try', 'good', 'ok', 'avoid'] as const),
            text: 'Test text, made up',
            timeSlots: [pick(['breakfast', 'lunch', 'afternoon', 'dinner', 'late_night'] as const)],
          },
        })
      }
      made += count
      for (let i = 0; i < bodies.length; i += 10) {
        await Promise.all(bodies.slice(i, i + 10).map((b) => client.post('/places', b)))
      }
    }
    return { baseURL: state.baseURL, session: state.maintainer.session, members, dataDir: join(dir, 'data'), stop }
  } catch (e) {
    await stop()
    throw e
  }
}
