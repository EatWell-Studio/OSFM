import { describe, expect, it } from 'vitest'
import type { City } from './cities'
import { attribution, basemapLanguage, basemapLayers, buildStyle, sourceId } from './style'

const origin = 'https://food.example'
const city: City = {
  id: 'example-town',
  bounds: [6.68, 0.3, 6.76, 0.36],
  center: [6.72, 0.33, 10],
  minzoom: 10,
  maxzoom: 15,
}
// The files in public/map/, served at /map/: only their names are needed, nothing is loaded.
const served = new Set(
  Object.keys(import.meta.glob('../../public/map/**/*', { query: '?url' })).map((path) =>
    path.replace('../../public', ''),
  ),
)

/** Every string anywhere in value. */
function strings(value: unknown): string[] {
  if (typeof value === 'string') {
    return [value]
  }
  if (typeof value === 'object' && value !== null) {
    return Object.values(value).flatMap(strings)
  }
  return []
}

describe.each(['zh-Hans', 'en'])('the basemap style (%s labels)', (lang) => {
  const style = buildStyle(city, lang, origin)

  it('draws no boundaries and no country or region labels (H2)', () => {
    const layers = basemapLayers(lang)
    expect(layers.length).toBeGreaterThan(50)
    for (const layer of layers) {
      expect(layer.id).not.toMatch(/boundar|country|region/)
      if ('source-layer' in layer) {
        expect(layer['source-layer']).not.toBe('boundaries')
      }
    }
  })

  it('loads nothing from another origin', () => {
    const urls = strings(style).filter((s) => /^[a-z][a-z0-9+.-]*:\/\//i.test(s) || s.startsWith('//'))
    expect(urls.length).toBeGreaterThan(0)
    for (const url of urls) {
      expect(url.startsWith(origin + '/')).toBe(true)
    }
    expect(style.glyphs).toBe(`${origin}/map/fonts/{fontstack}/{range}.pbf`)
    expect(style.sprite).toBe(`${origin}/map/sprites/light`)
    expect(style.sources[sourceId]).toMatchObject({
      tiles: [`${origin}/tiles/example-town/{z}/{x}/{y}.mvt`],
      bounds: city.bounds,
      minzoom: 10,
      maxzoom: 15,
      attribution,
    })
  })

  it('names only fonts the instance serves, besides the one drawn locally', () => {
    const fonts = new Set(strings(basemapLayers(lang)).filter((s) => s.startsWith('Noto Sans')))
    expect(fonts.size).toBeGreaterThan(0)
    for (const font of fonts) {
      // Devanagari names are pre-shaped in the tiles and drawn with local fonts when their glyphs are missing.
      if (font.includes('Devanagari')) {
        continue
      }
      expect(served.has(`/map/fonts/${font}/0-255.pbf`), font).toBe(true)
    }
  })
})

describe('the served map assets', () => {
  it('include the sprite at both pixel densities', () => {
    for (const file of ['light.json', 'light.png', 'light@2x.json', 'light@2x.png']) {
      expect(served.has(`/map/sprites/${file}`), file).toBe(true)
    }
  })
})

describe('attribution', () => {
  it('credits the OpenStreetMap contributors (A1)', () => {
    expect(attribution).toContain('OpenStreetMap</a> contributors')
  })
})

describe('basemapLanguage', () => {
  it('labels in Simplified Chinese for zh-CN and in English otherwise', () => {
    expect(basemapLanguage('zh-CN')).toBe('zh-Hans')
    expect(basemapLanguage('en')).toBe('en')
  })
})
