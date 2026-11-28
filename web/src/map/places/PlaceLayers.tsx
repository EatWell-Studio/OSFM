import { Layer, Source, useMap } from '@vis.gl/react-maplibre'
import type { GeoJSONSource, MapGeoJSONFeature, MapLayerMouseEvent, MapMouseEvent } from 'maplibre-gl'
import { useEffect, useMemo } from 'react'
import {
  circleLabelsLayer,
  circlesLayer,
  clusterCountsLayer,
  clustersLayer,
  interactiveLayerIds,
  layerIds,
  pinImageId,
  pinsLayer,
  placesGeoJSON,
  placesSource,
  placesSourceId,
} from './layers'
import { addPinImage } from './pin'
import type { Place } from './placesApi'

/** How far from a tap, in CSS pixels, a place still counts as tapped (a fingertip is larger than a pin's point). */
const tapTolerance = 8

/**
 * For the end-to-end tests only: when the page defines window.osfmTestHooks before the app starts (Playwright's init
 * script does), the map is put there, so that a test can query the features the layers rendered. Otherwise nothing.
 */
interface TestHooks {
  map?: unknown
}

/** The feature a tap at point opens, the most specific first (layers.ts interactiveLayerIds). */
function tappedFeature(features: MapGeoJSONFeature[]): MapGeoJSONFeature | undefined {
  for (const id of interactiveLayerIds) {
    const hit = features.find((f) => f.layer.id === id)
    if (hit) {
      return hit
    }
  }
  return undefined
}

/**
 * The places on the map (layers.ts), inside MapLibre's map. A tap on a place calls onOpen with its UUID; a tap on a
 * cluster zooms in until it falls apart.
 */
export function PlaceLayers({ places, onOpen }: { places: readonly Place[]; onOpen: (uuid: string) => void }) {
  const { current: mapRef } = useMap()
  const data = useMemo(() => placesGeoJSON(places), [places])

  useEffect(() => {
    if (!mapRef) {
      return
    }
    const map = mapRef.getMap()
    const hooks = (window as { osfmTestHooks?: TestHooks }).osfmTestHooks
    if (hooks && typeof hooks === 'object') {
      hooks.map = map
    }

    addPinImage(map)
    // A new style (another language of the basemap) starts without the pin.
    const onMissing = (e: { id: string }) => {
      if (e.id === pinImageId) {
        addPinImage(map)
      }
    }

    const near = (e: MapMouseEvent) => {
      const { x, y } = e.point
      return map.queryRenderedFeatures(
        [
          [x - tapTolerance, y - tapTolerance],
          [x + tapTolerance, y + tapTolerance],
        ],
        { layers: interactiveLayerIds.filter((id) => map.getLayer(id)) },
      )
    }
    const onClick = (e: MapLayerMouseEvent | MapMouseEvent) => {
      const feature = tappedFeature(near(e))
      if (!feature) {
        return
      }
      const props = feature.properties as Record<string, unknown>
      if (feature.layer.id === layerIds.clusters) {
        const source = map.getSource<GeoJSONSource>(placesSourceId)
        const center = (feature.geometry as { coordinates: [number, number] }).coordinates
        if (source && typeof props.cluster_id === 'number') {
          void source.getClusterExpansionZoom(props.cluster_id).then((zoom) => {
            map.easeTo({ center, zoom: zoom + 0.5 })
          })
        }
        return
      }
      if (typeof props.uuid === 'string') {
        onOpen(props.uuid)
      }
    }
    // The pointer over a place (with a mouse); not while dragging or moving, when it would only cost frames.
    const onMove = (e: MapMouseEvent) => {
      if (e.originalEvent.buttons !== 0 || map.isMoving()) {
        return
      }
      map.getCanvas().style.cursor = tappedFeature(near(e)) ? 'pointer' : ''
    }

    map.on('styleimagemissing', onMissing)
    map.on('click', onClick)
    map.on('mousemove', onMove)
    return () => {
      map.off('styleimagemissing', onMissing)
      map.off('click', onClick)
      map.off('mousemove', onMove)
      if (hooks && typeof hooks === 'object' && hooks.map === map) {
        delete hooks.map
      }
    }
  }, [mapRef, onOpen])

  return (
    <Source id={placesSourceId} {...placesSource(data)}>
      <Layer {...circlesLayer} />
      <Layer {...circleLabelsLayer} />
      <Layer {...pinsLayer} />
      <Layer {...clustersLayer} />
      <Layer {...clusterCountsLayer} />
    </Source>
  )
}
