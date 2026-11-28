// Creating places and recommendations through the API of the shared instance, as the later feature specs do with
// createPlace() to set up their data: a member creates a place, a second recommendation of the same member is refused
// with their own recommendation's UUID, another member adds theirs, and the duplicate hint finds the place. The browser
// flows are in the frontend's specs. Made-up data only.

import { expect, test } from '@playwright/test'
import type { CreatedRecommendation, NearbyPlaces, RecommendationInput } from '../src/api/types.gen.ts'
import { city } from './support/env.ts'
import { ApiFailure, apiAs, createPlace, inviteMember, type TestMember } from './support/shared.ts'

test.describe.configure({ mode: 'serial' })

const [minLon, minLat, maxLon, maxLat] = city.archive.bbox
// A point in the city, apart from where other specs put their places.
const lat = minLat + (maxLat - minLat) * 0.23
const lon = minLon + (maxLon - minLon) * 0.71

let alice: TestMember
let bob: TestMember

test.beforeAll(async () => {
  alice = await inviteMember()
  bob = await inviteMember()
})

/** The error code of a refused request, or null when it succeeded. */
async function refusal(request: Promise<unknown>): Promise<{ status: number; code: string | null } | null> {
  try {
    await request
    return null
  } catch (e) {
    if (e instanceof ApiFailure) {
      return { status: e.status, code: e.code }
    }
    throw e
  }
}

test('a member creates a place, recommends it once, and another member adds theirs', async () => {
  const created = await createPlace(alice, {
    name: 'Test Places API Noodles',
    lat,
    lon,
    category: 'restaurant',
    cuisine: ['noodle'],
    recommendation: { attitude: 'must_try', text: 'Test text, made up', timeSlots: ['lunch'] },
  })
  expect(created.place).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7/)
  expect(created.recommendation).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7/)

  const second: RecommendationInput = { attitude: 'ok', text: 'Test second text' }
  const path = `/places/${created.place}/recommendations`
  expect(await refusal(apiAs(alice).post(path, second))).toEqual({ status: 409, code: 'already_recommended' })
  const bobs = (await apiAs(bob).post(path, second)) as CreatedRecommendation
  expect(bobs.recommendation).not.toBe(created.recommendation)

  // The duplicate hint, for a name written a little differently a few metres away.
  const nearby = (await apiAs(bob).get(
    `/places/nearby?lat=${String(lat + 0.0002)}&lon=${String(lon)}&name=test+places-api+noodles`,
  )) as NearbyPlaces
  expect(nearby.places).toEqual([
    { uuid: created.place, name: 'Test Places API Noodles', category: 'restaurant', distance: 20 },
  ])
})

test('the server refuses what the form must not send', async () => {
  const recommendation: RecommendationInput = { attitude: 'good', text: 'Test text' }
  expect(await refusal(createPlace(alice, { name: 'Test Far Away', lat: 48.1, lon: 11.5, recommendation }))).toEqual({
    status: 400,
    code: 'outside_cities',
  })
  expect(
    await refusal(
      createPlace(alice, {
        name: 'Test Pen Name',
        lat,
        lon,
        recommendation: { ...recommendation, displayAs: 'pseudonym' },
      }),
    ),
  ).toEqual({ status: 400, code: 'no_pseudonym' })
  expect(
    await refusal(createPlace(alice, { name: 'Test Pharmacy', lat, lon, category: 'pharmacy', recommendation })),
  ).toEqual({ status: 400, code: 'invalid_request' })
})
