// The basemap style (docs/architecture.md §3.6, §4.3): the Protomaps basemap layers over the city's tiles, with
// glyphs and sprites served by the instance itself from public/map/. Nothing in it points to another origin.

import { layers, namedFlavor } from '@protomaps/basemaps'
import type { LayerSpecification, StyleSpecification } from 'maplibre-gl'
import type { City } from './cities'

export const sourceId = 'protomaps'

/**
 * The OpenStreetMap attribution, always shown (A1). The link is not loaded, only offered; it is the copyright page
 * the OSM licence asks attributions to point to.
 */
export const attribution =
  '© <a href="https://www.openstreetmap.org/copyright" rel="noreferrer">OpenStreetMap</a> contributors'

/**
 * Layers left out of the Protomaps style (H2): no boundaries of any kind, and no labels of countries or of
 * administrative regions. The tiles hold a boundaries layer; the style simply never draws it.
 */
const droppedSourceLayers = new Set(['boundaries'])
const droppedLayerIds = new Set(['places_country', 'places_region'])

/**
 * CJK text is drawn by the browser with the system's fonts (AD8): only Latin glyph ranges are served. MapLibre
 * renders a CJK character locally whenever this is set; the fonts are those of the app's font stack.
 */
export const localIdeographFontFamily =
  "'PingFang SC', 'Hiragino Sans GB', 'Noto Sans CJK SC', 'Microsoft YaHei', sans-serif"

/** The label language of the basemap for a UI language: names in Simplified Chinese for zh-CN. */
export function basemapLanguage(uiLanguage: string): string {
  return uiLanguage.startsWith('zh') ? 'zh-Hans' : 'en'
}

/** The Protomaps "light" layers in the given label language, without the dropped ones. */
export function basemapLayers(lang: string): LayerSpecification[] {
  const all = layers(sourceId, namedFlavor('light'), { lang }) as LayerSpecification[]
  return all.filter(
    (layer) =>
      !droppedLayerIds.has(layer.id) &&
      !(
        'source-layer' in layer &&
        typeof layer['source-layer'] === 'string' &&
        droppedSourceLayers.has(layer['source-layer'])
      ),
  )
}

/**
 * The style of one city. origin is the instance's own origin (location.origin): MapLibre fetches from absolute URLs,
 * and building them here keeps every request on the origin the page was loaded from.
 */
export function buildStyle(city: City, lang: string, origin: string): StyleSpecification {
  return {
    version: 8,
    glyphs: `${origin}/map/fonts/{fontstack}/{range}.pbf`,
    sprite: `${origin}/map/sprites/light`,
    sources: {
      [sourceId]: {
        type: 'vector',
        tiles: [`${origin}/tiles/${city.id}/{z}/{x}/{y}.mvt`],
        minzoom: city.minzoom,
        maxzoom: city.maxzoom,
        bounds: city.bounds,
        attribution,
      },
    },
    layers: basemapLayers(lang),
  }
}
