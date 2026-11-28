import { describe, expect, it } from 'vitest'
import { wgs84ToGcj02 } from './gcj02'

// Published WGS-84 / GCJ-02 pairs:
//   - eviltransform's test vector (github.com/googollee/eviltransform, go/transform_test.go), which its
//     implementations match to the last digit;
//   - gcoord's fixtures (github.com/hujiulong/gcoord, test/fixtures/china-cities.json), conversions of the Chinese map
//     providers rounded to 7 decimals, which every open implementation of the formula matches to well under a metre.
const exact = [{ wgs: { lat: 22.543847, lon: 113.912316 }, gcj: { lat: 22.540796131694766, lon: 113.9171764808363 } }]
const rounded = [
  { wgs: { lat: 33.0133556, lon: 114.0164322 }, gcj: { lat: 33.0115324, lon: 114.022306 } },
  { wgs: { lat: 27.242124, lon: 111.4622119 }, gcj: { lat: 27.2388981, lon: 111.4677938 } },
  { wgs: { lat: 37.0700925, lon: 114.498916 }, gcj: { lat: 37.0705911, lon: 114.5048503 } },
]

describe('wgs84ToGcj02', () => {
  it('matches the reference implementation', () => {
    for (const { wgs, gcj } of exact) {
      const p = wgs84ToGcj02(wgs)
      expect(p.lat).toBeCloseTo(gcj.lat, 9)
      expect(p.lon).toBeCloseTo(gcj.lon, 9)
    }
  })

  it("matches the map providers' conversion to about a metre", () => {
    for (const { wgs, gcj } of rounded) {
      const p = wgs84ToGcj02(wgs)
      // 1e-5 degrees is about 1.1 m of latitude.
      expect(Math.abs(p.lat - gcj.lat)).toBeLessThan(1e-5)
      expect(Math.abs(p.lon - gcj.lon)).toBeLessThan(1e-5)
    }
  })

  it('moves a point by some hundred metres', () => {
    const wgs = { lat: 30.54321, lon: 114.31234 }
    const p = wgs84ToGcj02(wgs)
    const metres = Math.hypot(
      (p.lat - wgs.lat) * 111_000,
      (p.lon - wgs.lon) * 111_000 * Math.cos((wgs.lat * Math.PI) / 180),
    )
    expect(metres).toBeGreaterThan(100)
    expect(metres).toBeLessThan(700)
  })
})
