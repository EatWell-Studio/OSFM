// The map (docs/architecture.md §4.3), loaded as its own chunk so that the app shell stays small.

import 'maplibre-gl/dist/maplibre-gl.css'
import { useQuery } from '@tanstack/react-query'
import { AttributionControl, Map as MapLibreMap, type MapRef, Marker } from '@vis.gl/react-maplibre'
import { LocateFixedIcon, MapPinIcon, MinusIcon, PlusIcon } from 'lucide-react'
import * as maplibregl from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?url'
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSearchParams } from 'react-router'
import { Button } from '@/components/ui/button'
import { instanceQuery } from '@/instance/instance'
import { type GeocodeResult, PlaceSearch } from '@/search/PlaceSearch'
import { type City, citiesQueryKey, contains, fetchCities, type MapCity, mergeCities, pickCity } from './cities'
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
 * session cookie: the tiles require a signed-in member.
 */
const transformRequest: maplibregl.RequestTransformFunction = (url) => ({ url, credentials: 'same-origin' })

export default function MapView() {
  const { t } = useTranslation('map')
  const cities = useQuery({ queryKey: citiesQueryKey, queryFn: ({ signal }) => fetchCities(signal) })
  // The display names of the cities, and the cities that have no tiles yet. Without them the archives are still shown.
  const instance = useQuery(instanceQuery)

  if (cities.isPending || instance.isPending) {
    return <MapMessage>{t('loading')}</MapMessage>
  }
  if (cities.isError) {
    return (
      <MapMessage>
        <p>{t('loadError')}</p>
        <Button
          variant="outline"
          onClick={() => {
            void cities.refetch()
          }}
        >
          {t('retry')}
        </Button>
      </MapMessage>
    )
  }
  const choices = mergeCities(cities.data, instance.data?.cities ?? [])
  if (choices.length === 0) {
    return <MapMessage>{t('noCities')}</MapMessage>
  }
  return <CityView choices={choices} />
}

function MapMessage({ children }: { children: ReactNode }) {
  return (
    <div role="status" className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center text-sm">
      {children}
    </div>
  )
}

/** The city chosen in the URL (or remembered, or the first), on the map or, without tiles yet, as a notice. */
function CityView({ choices }: { choices: MapCity[] }) {
  const { t } = useTranslation('map')
  const [searchParams, setSearchParams] = useSearchParams()
  const chosen = pickCity(choices, searchParams.get(cityParam), readRememberedCity())
  if (!chosen) {
    throw new Error('CityView needs at least one city')
  }

  useEffect(() => {
    rememberCity(chosen.id)
  }, [chosen.id])

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

  const select = (
    <div className="shrink-0">
      <label className="sr-only" htmlFor="map-city">
        {t('city')}
      </label>
      <select
        id="map-city"
        value={chosen.id}
        onChange={(event) => {
          selectCity(event.target.value)
        }}
        className="h-9 rounded-md border bg-background px-2 text-sm shadow-sm"
      >
        {choices.map((c) => (
          <option key={c.id} value={c.id}>
            {c.label}
          </option>
        ))}
      </select>
    </div>
  )

  if (!chosen.tiles) {
    return (
      <div className="relative h-full">
        <div className="absolute top-2.5 left-2.5 z-[2]">{select}</div>
        <MapMessage>{t('noTiles', { city: chosen.label })}</MapMessage>
      </div>
    )
  }
  const withTiles = choices.flatMap((c) => (c.tiles ? [c.tiles] : []))
  return <CityMap city={chosen.tiles} cities={withTiles} select={select} selectCity={selectCity} />
}

function CityMap({
  city,
  cities,
  select,
  selectCity,
}: {
  city: City
  cities: City[]
  select: ReactNode
  selectCity: (id: string) => void
}) {
  const { t, i18n } = useTranslation('map')
  const { t: tSearch } = useTranslation('search')
  const mapRef = useRef<MapRef>(null)
  const [position, setPosition] = useState<Position | null>(null)
  // The place search result the member picked, marked on the map until another city is shown.
  const [found, setFound] = useState<GeocodeResult | null>(null)
  // A position to center on once the map shows the city it lies in.
  const pendingCenter = useRef<Position | null>(null)

  const lang = basemapLanguage(i18n.language)
  const style = useMemo(() => buildStyle(city, lang, window.location.origin), [city, lang])
  const locale = useMemo(
    () => ({
      'Map.Title': t('controls.map'),
      'AttributionControl.ToggleAttribution': t('controls.toggleAttribution'),
    }),
    [t],
  )

  // Switching cities moves the map to the new city: the bounds changed with the props already.
  const shownCity = useRef(city.id)
  useEffect(() => {
    const map = mapRef.current
    if (!map || shownCity.current === city.id) {
      return
    }
    shownCity.current = city.id
    setFound(null)
    const center = pendingCenter.current
    pendingCenter.current = null
    if (center) {
      map.jumpTo({ center: [center.lon, center.lat], zoom: Math.max(map.getZoom(), 15) })
    } else {
      map.fitBounds(city.bounds, { animate: false })
    }
  }, [city])

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
  const showFound = useCallback((r: GeocodeResult) => {
    setFound(r)
    const map = mapRef.current
    map?.flyTo({ center: [r.lon, r.lat], zoom: Math.max(map.getZoom(), 16) })
  }, [])
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
              aria-label={t('locate.marker')}
              className="block size-4 rounded-full border-2 border-white bg-blue-600 shadow"
            />
          </Marker>
        )}
        {found && (
          <Marker longitude={found.lon} latitude={found.lat} anchor="bottom">
            <MapPinIcon
              role="img"
              aria-label={tSearch('marker', { name: found.name })}
              className="size-8 fill-red-600 text-white drop-shadow"
            />
          </Marker>
        )}
      </MapLibreMap>

      {/* The city and the place search across the top, left of the map buttons. */}
      <div className="absolute top-2.5 right-14 left-2.5 z-[3] flex items-start gap-2">
        {select}
        <PlaceSearch city={city.id} onSelect={showFound} className="min-w-0 flex-1" />
      </div>

      {/* Our own buttons rather than MapLibre's controls, whose icons are data: URLs that the CSP refuses (§4.7). */}
      <div className="absolute top-2.5 right-2.5 z-[2] flex flex-col gap-2">
        <Button
          variant="outline"
          size="icon"
          aria-label={t('controls.zoomIn')}
          onClick={() => mapRef.current?.zoomIn()}
          className="bg-background shadow-sm"
        >
          <PlusIcon aria-hidden="true" />
        </Button>
        <Button
          variant="outline"
          size="icon"
          aria-label={t('controls.zoomOut')}
          disabled={atMinZoom}
          onClick={() => mapRef.current?.zoomOut()}
          className="bg-background shadow-sm"
        >
          <MinusIcon aria-hidden="true" />
        </Button>
        <Button
          variant="outline"
          size="icon"
          aria-label={t('locate.button')}
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
        {locating && <p className="sr-only">{t('locate.locating')}</p>}
        {message && (
          <p className="rounded-md bg-foreground/85 px-3 py-2 text-sm text-background shadow">
            {t(`locate.${message}`)}
          </p>
        )}
      </div>
    </div>
  )
}
