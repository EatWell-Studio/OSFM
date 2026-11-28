import { describe, expect, it } from 'vitest'
import { navigationApps, navigationLink } from './navigation'

// Made-up places: one in central China, one in northern Germany.
const mainland = { lat: 30.54321, lon: 114.31234, name: 'Test Noodles & Co' }
const germany = { lat: 52.37452, lon: 9.73861, name: 'Test Imbiss' }

describe('navigationLink', () => {
  it('gives Amap GCJ-02 in mainland China, and says so', () => {
    expect(navigationLink('amap', mainland)).toBe(
      'https://uri.amap.com/marker?position=114.317791,30.540795&name=Test%20Noodles%20%26%20Co&coordinate=gaode&callnative=1',
    )
  })

  it('gives Google Maps GCJ-02 in mainland China', () => {
    expect(navigationLink('google', mainland)).toBe(
      'https://www.google.com/maps/search/?api=1&query=30.540795%2C114.317791',
    )
  })

  it('gives Apple Maps WGS-84 in mainland China', () => {
    expect(navigationLink('apple', mainland)).toBe(
      'https://maps.apple.com/?ll=30.54321,114.31234&q=Test%20Noodles%20%26%20Co',
    )
  })

  it('gives every app WGS-84 elsewhere', () => {
    expect(navigationLink('amap', germany)).toBe(
      'https://uri.amap.com/marker?position=9.73861,52.37452&name=Test%20Imbiss&coordinate=wgs84&callnative=1',
    )
    expect(navigationLink('apple', germany)).toBe('https://maps.apple.com/?ll=52.37452,9.73861&q=Test%20Imbiss')
    expect(navigationLink('google', germany)).toBe('https://www.google.com/maps/search/?api=1&query=52.37452%2C9.73861')
  })

  it('keeps the name inside its parameter', () => {
    const tricky = { ...germany, name: 'Test <b>A&B</b> #1 ?x=y' }
    for (const app of navigationApps) {
      const url = new URL(navigationLink(app, tricky))
      expect(url.hash).toBe('')
      expect(url.searchParams.has('x')).toBe(false)
      if (app !== 'google') {
        expect(url.searchParams.get(app === 'amap' ? 'name' : 'q')).toBe(tricky.name)
      }
    }
  })
})
