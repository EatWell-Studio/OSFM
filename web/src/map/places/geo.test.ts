import { describe, expect, it } from 'vitest'
import { byDistance } from '@/list/byDistance'
import type { Bounds } from '../cities'
import { clipBox, containsBox, distanceMetres, fetchBox, metresPerPixel, radiusAtZoom0, roundedDistance } from './geo'
import { circleRadius, minCircleRadius, placesGeoJSON } from './layers'
import { bboxParam, parsePlaces, type Place } from './placesApi'

const city: Bounds = [7.02, 0.31, 7.1, 0.37]

function place(uuid: string, name: string, lon: number, lat: number, precision: Place['precision'] = 'exact'): Place {
  return {
    uuid,
    name,
    lon,
    lat,
    precision,
    category: '',
    counts: { mustTry: 0, good: 1, ok: 0, avoid: 0 },
    timeSlots: [],
  }
}

describe('circle sizes', () => {
  it('converts metres to pixels at the zoom level and latitude', () => {
    // At the equator, zoom 0: the world (about 40 075 km) is 512 pixels wide.
    expect(metresPerPixel(0, 0)).toBeCloseTo(78_271.5, 0)
    expect(metresPerPixel(0, 16)).toBeCloseTo(1.194, 3)
    // Half as many metres per pixel at 60°.
    expect(metresPerPixel(60, 16)).toBeCloseTo(0.597, 3)
    // 50 m at zoom 16 near the equator: about 42 pixels.
    expect(radiusAtZoom0(50, 0) * 2 ** 16).toBeCloseTo(41.9, 1)
  })

  it('draws approximate places and areas with their radius, exact places without one', () => {
    const features = placesGeoJSON([
      place('a', 'Test Exact', 7.05, 0.34),
      place('b', 'Test Approx', 7.05, 0.34, 'approx'),
      place('c', 'Test Area', 7.05, 0.34, 'area'),
    ]).features
    expect(features.map((f) => f.properties.r0 * 2 ** 16)).toEqual([0, expect.closeTo(41.9, 1), expect.closeTo(251, 0)])
    expect(features[0]?.geometry.coordinates).toEqual([7.05, 0.34])
  })

  it('interpolates the radius per zoom level, never below the minimum', () => {
    const expression = circleRadius()
    expect(expression.slice(0, 3)).toEqual(['interpolate', ['exponential', 2], ['zoom']])
    expect(expression[3]).toBe(10)
    expect(expression[4]).toEqual(['max', minCircleRadius, ['*', ['get', 'r0'], 1024]])
  })
})

describe('the area the map asks for', () => {
  it('grows the view by half its size on each side, rounded outwards to 0.001°', () => {
    expect(fetchBox([7.0503, 0.3402, 7.0603, 0.3502], city)).toEqual([7.045, 0.335, 7.066, 0.356])
  })

  it('stays within the city', () => {
    expect(fetchBox([7.0, 0.3, 7.2, 0.4], city)).toEqual(city)
    expect(fetchBox([8, 1, 8.1, 1.1], city)).toEqual(city)
  })

  it('knows when a view is inside the area loaded', () => {
    expect(containsBox(city, [7.03, 0.32, 7.04, 0.33])).toBe(true)
    expect(containsBox([7.03, 0.32, 7.04, 0.33], city)).toBe(false)
    expect(clipBox([7.0, 0.35, 7.05, 0.4], city)).toEqual([7.02, 0.35, 7.05, 0.37])
    expect(clipBox([8, 1, 9, 2], city)).toBeNull()
  })

  it('sends the box with at most 5 decimals', () => {
    expect(bboxParam([7.123456789, 0.1, 7.2, 0.300004])).toBe('7.12346,0.1,7.2,0.3')
  })
})

describe('distances in the list', () => {
  it('rounds to 10 m below 1 km and to 0.1 km above (H3)', () => {
    expect(roundedDistance(0)).toEqual({ unit: 'm', value: 0 })
    expect(roundedDistance(4)).toEqual({ unit: 'm', value: 0 })
    expect(roundedDistance(127)).toEqual({ unit: 'm', value: 130 })
    expect(roundedDistance(994)).toEqual({ unit: 'm', value: 990 })
    expect(roundedDistance(996)).toEqual({ unit: 'km', value: 1 })
    expect(roundedDistance(1249)).toEqual({ unit: 'km', value: 1.2 })
    expect(roundedDistance(12_345)).toEqual({ unit: 'km', value: 12.3 })
  })

  it('measures great-circle distances', () => {
    // 0.001° of latitude is about 111 m.
    expect(distanceMetres({ lon: 7.05, lat: 0.34 }, { lon: 7.05, lat: 0.341 })).toBeCloseTo(111.2, 0)
  })

  it('sorts by distance only, then by name', () => {
    const origin = { lon: 7.05, lat: 0.34 }
    const sorted = byDistance(
      [
        place('1', 'Test Far', 7.06, 0.34),
        place('2', 'Test B Near', 7.05, 0.3401),
        place('3', 'Test A Near', 7.05, 0.3401),
        place('4', 'Test Here', 7.05, 0.34),
      ],
      origin,
    )
    expect(sorted.map((s) => s.place.name)).toEqual(['Test Here', 'Test A Near', 'Test B Near', 'Test Far'])
  })
})

describe('the places of the server', () => {
  it('keeps the well-formed entries and draws an unknown precision as an area', () => {
    const counts = { mustTry: 1, good: 0, ok: 0, avoid: 0 }
    const places = parsePlaces([
      {
        uuid: 'u1',
        name: 'Test One',
        lat: 0.34,
        lon: 7.05,
        precision: 'exact',
        category: 'cafe',
        counts,
        timeSlots: ['lunch'],
      },
      {
        uuid: 'u2',
        name: 'Test Two',
        lat: 0.34,
        lon: 7.05,
        precision: 'somewhere',
        category: '',
        counts,
        timeSlots: [],
      },
      { uuid: 'u3', name: 'Test Bad', lat: 'x', lon: 7.05, precision: 'exact', counts },
      { uuid: 'u4', name: 'Test Bad Counts', lat: 0.34, lon: 7.05, precision: 'exact', counts: { mustTry: -1 } },
    ])
    expect(places.map((p) => [p.uuid, p.precision])).toEqual([
      ['u1', 'exact'],
      ['u2', 'area'],
    ])
    expect(() => parsePlaces({})).toThrow()
  })
})
