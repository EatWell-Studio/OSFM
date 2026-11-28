// The places on the map (A2, B3; docs/architecture.md §4.3): one GeoJSON source with MapLibre's clustering and the
// layers that draw it. An exact place is a pin; an approximate place ("approx", about 50 m) and an area (300 m) are
// translucent circles whose radius stays the same on the ground at every zoom level, never a pin (H3).

import type {
  CircleLayerSpecification,
  ExpressionSpecification,
  GeoJSONSourceSpecification,
  SymbolLayerSpecification,
} from 'maplibre-gl'
import type { Place } from './placesApi'
import { precisionRadius, radiusAtZoom0 } from './geo'

export const placesSourceId = 'osfm-places'
export const pinImageId = 'osfm-pin'

export const layerIds = {
  clusters: 'osfm-place-clusters',
  clusterCounts: 'osfm-place-cluster-counts',
  circles: 'osfm-place-circles',
  circleLabels: 'osfm-place-circle-labels',
  pins: 'osfm-place-pins',
} as const

/** The layers a tap opens something on, the most specific first: a pin, a circle's label, a cluster, a circle. */
export const interactiveLayerIds = [layerIds.pins, layerIds.circleLabels, layerIds.clusters, layerIds.circles]

/** Up to this zoom level nearby places are clustered; from the next one on each place is drawn on its own. */
export const clusterMaxZoom = 14

/** The properties of a place's feature. */
export interface PlaceProperties {
  uuid: string
  name: string
  precision: Place['precision']
  /** For a circle: its radius in CSS pixels at zoom 0 (geo.ts radiusAtZoom0); 0 for a pin. */
  r0: number
}

export interface PlaceFeature {
  type: 'Feature'
  geometry: { type: 'Point'; coordinates: [number, number] }
  properties: PlaceProperties
}

export interface PlaceFeatureCollection {
  type: 'FeatureCollection'
  features: PlaceFeature[]
}

/** The places as GeoJSON points, with what the layers need and nothing else. */
export function placesGeoJSON(places: readonly Place[]): PlaceFeatureCollection {
  return {
    type: 'FeatureCollection',
    features: places.map((p) => ({
      type: 'Feature',
      geometry: { type: 'Point', coordinates: [p.lon, p.lat] },
      properties: {
        uuid: p.uuid,
        name: p.name,
        precision: p.precision,
        r0: p.precision === 'exact' ? 0 : radiusAtZoom0(precisionRadius[p.precision], p.lat),
      },
    })),
  }
}

export function placesSource(data: PlaceFeatureCollection): GeoJSONSourceSpecification {
  return { type: 'geojson', data, cluster: true, clusterMaxZoom, clusterRadius: 48 }
}

const notClustered: ExpressionSpecification = ['!', ['has', 'point_count']]

/** A circle is never drawn smaller than this, in CSS pixels, so that it stays visible and tappable when zoomed out. */
export const minCircleRadius = 6

/**
 * The circle's radius in CSS pixels: r0 · 2^zoom, at least minCircleRadius. MapLibre allows the zoom only at the top of
 * an interpolation, so the minimum is applied at each whole zoom level; between them the exponential interpolation
 * (base 2) is exact wherever the minimum does not apply.
 */
export function circleRadius(): ExpressionSpecification {
  const stops: (number | ExpressionSpecification)[] = []
  for (let z = 10; z <= 24; z++) {
    stops.push(z, ['max', minCircleRadius, ['*', ['get', 'r0'], 2 ** z]])
  }
  return ['interpolate', ['exponential', 2], ['zoom'], ...stops] as ExpressionSpecification
}

const colors = {
  place: '#dc2626',
  cluster: '#b91c1c',
  halo: '#ffffff',
  text: '#1f2937',
}

// Latin glyphs are served by the instance (public/map/fonts); CJK names are drawn with the system's fonts (style.ts).
const regular = ['Noto Sans Regular']
const medium = ['Noto Sans Medium']

/**
 * Names are shown from this zoom level on, where a screen holds few places. Placing hundreds of labels is what costs
 * the most frames when moving the map (measured with 1000 places: without labels the places cost about nothing beyond
 * the basemap), and below this level the names would mostly hide each other anyway.
 */
export const labelMinZoom = 16

const nameLabel: SymbolLayerSpecification['layout'] = {
  'text-field': ['step', ['zoom'], '', labelMinZoom, ['get', 'name']],
  'text-font': regular,
  'text-size': 12,
  'text-max-width': 8,
  'text-optional': true,
}

export const clustersLayer: CircleLayerSpecification = {
  id: layerIds.clusters,
  type: 'circle',
  source: placesSourceId,
  filter: ['has', 'point_count'],
  paint: {
    'circle-color': colors.cluster,
    'circle-opacity': 0.85,
    'circle-radius': ['step', ['get', 'point_count'], 14, 10, 17, 50, 21, 200, 25],
    'circle-stroke-color': colors.halo,
    'circle-stroke-width': 2,
  },
}

export const clusterCountsLayer: SymbolLayerSpecification = {
  id: layerIds.clusterCounts,
  type: 'symbol',
  source: placesSourceId,
  filter: ['has', 'point_count'],
  layout: {
    'text-field': ['get', 'point_count_abbreviated'],
    'text-font': medium,
    'text-size': 12,
    'text-allow-overlap': true,
  },
  paint: { 'text-color': colors.halo },
}

/** Approximate places and areas: translucent circles, the larger beneath the smaller. */
export const circlesLayer: CircleLayerSpecification = {
  id: layerIds.circles,
  type: 'circle',
  source: placesSourceId,
  filter: ['all', notClustered, ['!=', ['get', 'precision'], 'exact']],
  layout: { 'circle-sort-key': ['-', ['get', 'r0']] },
  paint: {
    'circle-radius': circleRadius(),
    'circle-color': colors.place,
    'circle-opacity': ['match', ['get', 'precision'], 'area', 0.12, 0.2],
    'circle-stroke-color': colors.place,
    'circle-stroke-opacity': 0.6,
    'circle-stroke-width': 1.5,
    'circle-pitch-alignment': 'map',
  },
}

export const circleLabelsLayer: SymbolLayerSpecification = {
  id: layerIds.circleLabels,
  type: 'symbol',
  source: placesSourceId,
  filter: ['all', notClustered, ['!=', ['get', 'precision'], 'exact']],
  layout: { ...nameLabel, 'text-anchor': 'center' },
  paint: { 'text-color': colors.text, 'text-halo-color': colors.halo, 'text-halo-width': 1.5 },
}

/** Exact places: the pin (pin.ts), its tip on the point, with the name below. */
export const pinsLayer: SymbolLayerSpecification = {
  id: layerIds.pins,
  type: 'symbol',
  source: placesSourceId,
  filter: ['all', notClustered, ['==', ['get', 'precision'], 'exact']],
  layout: {
    ...nameLabel,
    'icon-image': pinImageId,
    'icon-anchor': 'bottom',
    'icon-allow-overlap': true,
    'text-anchor': 'top',
    'text-offset': [0, 0.25],
  },
  paint: { 'text-color': colors.text, 'text-halo-color': colors.halo, 'text-halo-width': 1.5 },
}
