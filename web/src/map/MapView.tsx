// The map (docs/architecture.md §4.3), loaded as its own chunk so that the app shell stays small.

import 'maplibre-gl/dist/maplibre-gl.css'
import { useQuery } from '@tanstack/react-query'
import { AttributionControl, Map as MapLibreMap, type MapRef, Marker } from '@vis.gl/react-maplibre'
import { LocateFixedIcon, MinusIcon, PlusIcon } from 'lucide-react'
import * as maplibregl from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?url'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router'
import { Button } from '@/components/ui/button'
import { type City, citiesQueryKey, cityLabel, contains, fetchCities, pickCity } from './cities'
import { basemapLanguage, buildStyle, localIdeographFontFamily } from './style'
import { type Position, useLocate } from './useLocate'

/** No zoom below city level (H2). */
export const minZoom = 10

const cityParam = 'city'
const rememberedCityKey = 'osfm.map.city'

function readRememberedCity(): string | null {
  try {
    return localStorage.getItem(rememberedCityKey)
  } catch {
    return null
  }
}

function rememberCity(id: string) {
  try {
    localStorage.setItem(rememberedCityKey, id)
  } catch {
    // Remembering the city is a convenience only.
  }
}

/**
 * Every request MapLibre makes goes to the page's own origin (the style holds only same-origin URLs) and carries the
 * session cookie, as the tiles will require a signed-in member (T007).
 */
const transformRequest: maplibregl.RequestTransformFunction = (url) => ({ url, credentials: 'same-origin' })

export default function MapView() {
  const { t } = useTranslation()
  const cities = useQuery({ queryKey: citiesQueryKey, queryFn: ({ signal }) => fetchCities(signal) })

  if (cities.isPending) {
    return <MapMessage>{t('map.loading')}</MapMessage>
  }
  if (cities.isError) {
    return (
      <MapMessage>
        <p>{t('map.loadError')}</p>
        <Button
          variant="outline"
          onClick={() => {
            void cities.refetch()
          }}
        >
          {t('map.retry')}
        </Button>
      </MapMessage>
    )
  }
  if (cities.data.length === 0) {
    return <MapMessage>{t('map.noCities')}</MapMessage>
  }
  return <CityMap cities={cities.data} />
}

function MapMessage({ children }: { children: React.ReactNode }) {
  return (
    <div role="status" className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center text-sm">
      {children}
    </div>
  )
}

function CityMap({ cities }: { cities: City[] }) {
  const { t, i18n } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  const mapRef = useRef<MapRef>(null)
  const [position, setPosition] = useState<Position | null>(null)
  // A position to center on once the map shows the city it lies in.
  const pendingCenter = useRef<Position | null>(null)

  const city = pickCity(cities, searchParams.get(cityParam), readRememberedCity())
  if (!city) {
    throw new Error('CityMap needs at least one city')
  }

  const lang = basemapLanguage(i18n.language)
  const style = useMemo(() => buildStyle(city, lang, window.location.origin), [city, lang])
  const locale = useMemo(
    () => ({
      'Map.Title': t('map.controls.map'),
      'AttributionControl.ToggleAttribution': t('map.controls.toggleAttribution'),
    }),
    [t],
  )

  useEffect(() => {
    rememberCity(city.id)
  }, [city.id])

  // Switching cities moves the map to the new city: the bounds changed with the props already.
  const shownCity = useRef(city.id)
  useEffect(() => {
    const map = mapRef.current
    if (!map || shownCity.current === city.id) {
      return
    }
    shownCity.current = city.id
    const center = pendingCenter.current
    pendingCenter.current = null
    if (center) {
      map.jumpTo({ center: [center.lon, center.lat], zoom: Math.max(map.getZoom(), 15) })
    } else {
      map.fitBounds(city.bounds, { animate: false })
    }
  }, [city])

  const selectCity = useCallback(
    (id: string) => {
      setSearchParams(
        (params) => {
          params.set(cityParam, id)
          return params
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  /** Centers on the member's position, switching to the city it lies in; false when it lies in none. */
  const showPosition = useCallback(
    (p: Position): boolean => {
      const target = contains(city.bounds, p.lon, p.lat) ? city : cities.find((c) => contains(c.bounds, p.lon, p.lat))
      if (!target) {
        return false
      }
      setPosition(p)
      if (target.id === city.id) {
        const map = mapRef.current
        map?.flyTo({ center: [p.lon, p.lat], zoom: Math.max(map.getZoom(), 15) })
      } else {
        pendingCenter.current = p
        selectCity(target.id)
      }
      return true
    },
    [cities, city, selectCity],
  )
  const { locate, locating, message } = useLocate(showPosition)
  const [atMinZoom, setAtMinZoom] = useState(false)
  const updateZoomLimit = useCallback(() => {
    const map = mapRef.current
    if (map) {
      setAtMinZoom(map.getZoom() <= map.getMinZoom())
    }
  }, [])

  return (
    <div className="relative h-full">
      <MapLibreMap
        ref={mapRef}
        mapLib={maplibregl}
        workerUrl={workerUrl}
        mapStyle={style}
        initialViewState={{ bounds: city.bounds }}
        minZoom={minZoom}
        maxBounds={city.bounds}
        localIdeographFontFamily={localIdeographFontFamily}
        transformRequest={transformRequest}
        attributionControl={false}
        dragRotate={false}
        pitchWithRotate={false}
        touchPitch={false}
        locale={locale}
        onLoad={updateZoomLimit}
        onZoomEnd={updateZoomLimit}
        style={{ width: '100%', height: '100%' }}
      >
        {/* Expanded, so the OpenStreetMap attribution is always visible (A1). */}
        <AttributionControl compact={false} position="bottom-right" />
        {position && (
          <Marker longitude={position.lon} latitude={position.lat}>
            <span
              role="img"
              aria-label={t('map.locate.marker')}
              className="block size-4 rounded-full border-2 border-white bg-blue-600 shadow"
            />
          </Marker>
        )}
      </MapLibreMap>

      <div className="absolute top-2.5 left-2.5 z-[2]">
        <label className="sr-only" htmlFor="map-city">
          {t('map.city')}
        </label>
        <select
          id="map-city"
          value={city.id}
          onChange={(event) => {
            selectCity(event.target.value)
          }}
          className="h-9 rounded-md border bg-background px-2 text-sm shadow-sm"
        >
          {cities.map((c) => (
            <option key={c.id} value={c.id}>
              {cityLabel(c)}
            </option>
          ))}
        </select>
      </div>

      {/* Our own buttons rather than MapLibre's controls, whose icons are data: URLs that the CSP refuses (§4.7). */}
      <div className="absolute top-2.5 right-2.5 z-[2] flex flex-col gap-2">
        <Button
          variant="outline"
          size="icon"
          aria-label={t('map.controls.zoomIn')}
          onClick={() => mapRef.current?.zoomIn()}
          className="bg-background shadow-sm"
        >
          <PlusIcon aria-hidden="true" />
        </Button>
        <Button
          variant="outline"
          size="icon"
          aria-label={t('map.controls.zoomOut')}
          disabled={atMinZoom}
          onClick={() => mapRef.current?.zoomOut()}
          className="bg-background shadow-sm"
        >
          <MinusIcon aria-hidden="true" />
        </Button>
        <Button
          variant="outline"
          size="icon"
          aria-label={t('map.locate.button')}
          disabled={locating}
          onClick={locate}
          className="mt-2 bg-background shadow-sm"
        >
          <LocateFixedIcon aria-hidden="true" className={locating ? 'animate-pulse' : undefined} />
        </Button>
      </div>

      <div
        role="status"
        aria-live="polite"
        className="pointer-events-none absolute inset-x-0 top-14 z-[2] flex justify-center px-16"
      >
        {locating && <p className="sr-only">{t('map.locate.locating')}</p>}
        {message && (
          <p className="rounded-md bg-foreground/85 px-3 py-2 text-sm text-background shadow">
            {t(`map.locate.${message}`)}
          </p>
        )}
      </div>
    </div>
  )
}
