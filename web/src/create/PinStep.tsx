// The first step of a new place (B1): a crosshair fixed at the centre of the map, which the member drags under the
// place. The map starts at the member's position when they have allowed the browser to tell it and it lies in a city,
// else where the map was last; the place search (B7, T039) moves it to a result. The buttons are at the bottom, within
// reach of a thumb. No coordinates are ever shown (H3); the point goes to the next step only.
//
// Loaded as its own chunk with MapLibre, like the map page.

import 'maplibre-gl/dist/maplibre-gl.css'
import { useQuery } from '@tanstack/react-query'
import { AttributionControl, Map as MapLibreMap, type MapRef, type ViewStateChangeEvent } from '@vis.gl/react-maplibre'
import { CrosshairIcon, LocateFixedIcon, MinusIcon, PlusIcon } from 'lucide-react'
import * as maplibregl from 'maplibre-gl'
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?url'
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { type InstanceCity, instanceQuery } from '@/instance/instance'
import { type City, citiesQueryKey, contains, fetchCities, mergeCities, pickCity } from '@/map/cities'
import { lastView } from '@/map/places/view'
import { readRememberedCity } from '@/map/rememberedCity'
import { basemapLanguage, buildStyle, localIdeographFontFamily } from '@/map/style'
import { type Position, useLocate } from '@/map/useLocate'
import { type GeocodeResult, PlaceSearch } from '@/search/PlaceSearch'
import type { Point } from './placeForm'

/** As the map page: no zoom below city level (H2). */
const minZoom = 10
/** The zoom the crosshair starts at, at least: streets and houses are told apart. */
const pinZoom = 16

/** Every request goes to the page's own origin with the session cookie, as on the map page. */
const transformRequest: maplibregl.RequestTransformFunction = (url) => ({ url, credentials: 'same-origin' })

/** Where the crosshair was: to start there again when the member comes back from the next step. */
export interface PinView {
  city: string
  center: [number, number]
  zoom: number
  point: Point
}

/** A city a place can be created in: one of the instance settings (the server checks its box, H2) with tiles. */
interface PinCity {
  id: string
  label: string
  tiles: City
  bbox: InstanceCity['bbox']
}

const inBox = (bbox: InstanceCity['bbox'], p: { lon: number; lat: number }) => contains(bbox, p.lon, p.lat)

/**
 * The step: loads the cities, then the map of the city the crosshair starts in. onConfirm gets the point under the
 * crosshair (or the search result picked and not moved from) and the view, for coming back.
 */
export default function PinStep({ start, onConfirm }: { start: PinView | null; onConfirm: (view: PinView) => void }) {
  const { t } = useTranslation('create')
  const archives = useQuery({ queryKey: citiesQueryKey, queryFn: ({ signal }) => fetchCities(signal) })
  const instance = useQuery(instanceQuery)

  const choices = useMemo<PinCity[]>(() => {
    const configured = instance.data?.cities ?? []
    return mergeCities(archives.data ?? [], configured).flatMap((c) => {
      const settings = configured.find((s) => s.slug === c.id)
      return c.tiles && settings ? [{ id: c.id, label: c.label, tiles: c.tiles, bbox: settings.bbox }] : []
    })
  }, [archives.data, instance.data])

  if (archives.isPending || instance.isPending) {
    return <PinMessage>{t('pin.loading')}</PinMessage>
  }
  if (archives.isError || instance.isError) {
    return (
      <PinMessage>
        <p>{t('pin.loadError')}</p>
        <Button
          variant="outline"
          onClick={() => {
            void archives.refetch()
            void instance.refetch()
          }}
        >
          {t('pin.retry')}
        </Button>
      </PinMessage>
    )
  }
  if (choices.length === 0) {
    return <PinMessage>{t('pin.noCity')}</PinMessage>
  }
  return <PinCities choices={choices} start={start} onConfirm={onConfirm} />
}

function PinMessage({ children }: { children: ReactNode }) {
  return (
    <div role="status" className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center text-sm">
      {children}
    </div>
  )
}

/** Where the map opens: the view to come back to, else the map's last view of the remembered city, else its centre. */
function initialView(
  choices: PinCity[],
  start: PinView | null,
): { city: PinCity; center: [number, number]; zoom: number } {
  const back = start ? choices.find((c) => c.id === start.city) : undefined
  if (start && back) {
    return { city: back, center: start.center, zoom: start.zoom }
  }
  const city = pickCity(choices, null, readRememberedCity())
  if (!city) {
    throw new Error('PinStep needs at least one city')
  }
  const view = lastView(city.id)
  if (view) {
    return { city, center: view.center, zoom: Math.max(view.zoom, pinZoom) }
  }
  return { city, center: [city.tiles.center[0], city.tiles.center[1]], zoom: pinZoom }
}

function PinCities({
  choices,
  start,
  onConfirm,
}: {
  choices: PinCity[]
  start: PinView | null
  onConfirm: (view: PinView) => void
}) {
  const [view, setView] = useState(() => initialView(choices, start))
  // Once the member moved the map, their position no longer takes over.
  const touched = useRef(start !== null)

  const showPosition = useCallback(
    (p: Position): boolean => {
      const city = choices.find((c) => inBox(c.bbox, p))
      if (!city) {
        return false
      }
      setView({ city, center: [p.lon, p.lat], zoom: Math.max(pinZoom + 1, 17) })
      return true
    },
    [choices],
  )

  // The member's position, only when they allowed it already: the page never asks for it by itself.
  useEffect(() => {
    if (touched.current || !('geolocation' in navigator) || !('permissions' in navigator)) {
      return
    }
    let cancelled = false
    navigator.permissions
      .query({ name: 'geolocation' })
      .then((status) => {
        if (cancelled || status.state !== 'granted') {
          return
        }
        navigator.geolocation.getCurrentPosition(
          ({ coords }) => {
            if (!cancelled && !touched.current) {
              showPosition({ lon: coords.longitude, lat: coords.latitude })
            }
          },
          () => undefined,
          { enableHighAccuracy: true, timeout: 15000, maximumAge: 60000 },
        )
      })
      .catch(() => undefined)
    return () => {
      cancelled = true
    }
  }, [showPosition])

  return (
    <PinMap
      // Another city, or the member's position: a map of its own, starting there.
      key={`${view.city.id} ${String(view.center[0])} ${String(view.center[1])}`}
      city={view.city}
      choices={choices}
      center={view.center}
      zoom={view.zoom}
      startSource={start?.city === view.city.id ? start.point : null}
      onTouched={() => {
        touched.current = true
      }}
      onCity={(id) => {
        const city = choices.find((c) => c.id === id)
        if (city) {
          touched.current = true
          setView({ city, center: [city.tiles.center[0], city.tiles.center[1]], zoom: pinZoom })
        }
      }}
      showPosition={(p) => {
        touched.current = true
        return showPosition(p)
      }}
      onConfirm={onConfirm}
    />
  )
}

function PinMap({
  city,
  choices,
  center,
  zoom,
  startSource,
  onTouched,
  onCity,
  showPosition,
  onConfirm,
}: {
  city: PinCity
  choices: PinCity[]
  center: [number, number]
  zoom: number
  startSource: Point | null
  onTouched: () => void
  onCity: (id: string) => void
  showPosition: (p: Position) => boolean
  onConfirm: (view: PinView) => void
}) {
  const { t, i18n } = useTranslation('create')
  const mapRef = useRef<MapRef>(null)
  // The search result picked, while the map has not been moved by hand since (B12: the point's source).
  const [found, setFound] = useState<{ lat: number; lon: number } | null>(
    startSource?.source === 'search' ? { lat: startSource.lat, lon: startSource.lon } : null,
  )
  const [inside, setInside] = useState(() => inBox(city.bbox, { lon: center[0], lat: center[1] }))
  const lang = basemapLanguage(i18n.language)
  const style = useMemo(() => buildStyle(city.tiles, lang, window.location.origin), [city.tiles, lang])
  const locale = useMemo(
    () => ({ 'Map.Title': t('pin.map'), 'AttributionControl.ToggleAttribution': t('pin.toggleAttribution') }),
    [t],
  )
  const [initialViewState] = useState({ longitude: center[0], latitude: center[1], zoom })
  const { locate, locating, message } = useLocate(showPosition)

  const check = useCallback(() => {
    const map = mapRef.current
    if (map) {
      const c = map.getCenter()
      setInside(inBox(city.bbox, { lon: c.lng, lat: c.lat }))
    }
  }, [city.bbox])

  const showFound = useCallback((r: GeocodeResult) => {
    const map = mapRef.current
    if (!map) {
      return
    }
    setFound({ lat: r.lat, lon: r.lon })
    map.jumpTo({ center: [r.lon, r.lat], zoom: Math.max(map.getZoom(), 17) })
  }, [])

  const confirm = () => {
    const map = mapRef.current
    if (!map) {
      return
    }
    const c = map.getCenter()
    const point: Point = found
      ? { lat: found.lat, lon: found.lon, source: 'search' }
      : { lat: c.lat, lon: c.lng, source: 'crosshair' }
    onConfirm({ city: city.id, center: [c.lng, c.lat], zoom: map.getZoom(), point })
  }

  return (
    <div className="relative h-full">
      <MapLibreMap
        ref={mapRef}
        mapLib={maplibregl}
        workerUrl={workerUrl}
        mapStyle={style}
        initialViewState={initialViewState}
        minZoom={minZoom}
        maxBounds={city.tiles.bounds}
        localIdeographFontFamily={localIdeographFontFamily}
        transformRequest={transformRequest}
        attributionControl={false}
        dragRotate={false}
        pitchWithRotate={false}
        touchPitch={false}
        locale={locale}
        onLoad={(e) => {
          // For the end-to-end tests, as on the map page (src/map/places/PlaceLayers.tsx).
          const hooks = (window as { osfmTestHooks?: { map?: unknown } }).osfmTestHooks
          if (hooks && typeof hooks === 'object') {
            hooks.map = e.target
          }
          check()
        }}
        onMoveStart={(e: ViewStateChangeEvent) => {
          // Moved by hand (a drag, a pinch, the wheel): the point is the crosshair's again. Moves of the page's own
          // (the search result, the zoom buttons) have no original event.
          if (e.originalEvent) {
            setFound(null)
            onTouched()
          }
        }}
        onMoveEnd={check}
        style={{ width: '100%', height: '100%' }}
      >
        <AttributionControl compact={false} position="bottom-right" />
      </MapLibreMap>

      {/* The crosshair: the point is the centre of the map. */}
      <div className="pointer-events-none absolute top-1/2 left-1/2 z-[1] -translate-x-1/2 -translate-y-1/2">
        <CrosshairIcon
          aria-hidden="true"
          strokeWidth={1.5}
          className="size-12 text-red-600 drop-shadow-[0_0_2px_white]"
        />
      </div>

      <div className="absolute top-2.5 right-14 left-2.5 z-[3] flex items-start gap-2">
        {choices.length > 1 && (
          <div className="shrink-0">
            <label className="sr-only" htmlFor="pin-city">
              {t('pin.city')}
            </label>
            <select
              id="pin-city"
              value={city.id}
              onChange={(e) => {
                onCity(e.target.value)
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
        )}
        <PlaceSearch city={city.id} onSelect={showFound} className="min-w-0 flex-1" />
      </div>

      <div className="absolute top-2.5 right-2.5 z-[2] flex flex-col gap-2">
        <Button
          variant="outline"
          size="icon"
          aria-label={t('pin.zoomIn')}
          onClick={() => mapRef.current?.zoomIn()}
          className="bg-background shadow-sm"
        >
          <PlusIcon aria-hidden="true" />
        </Button>
        <Button
          variant="outline"
          size="icon"
          aria-label={t('pin.zoomOut')}
          onClick={() => mapRef.current?.zoomOut()}
          className="bg-background shadow-sm"
        >
          <MinusIcon aria-hidden="true" />
        </Button>
      </div>

      {/* At thumb's reach, above the attribution. */}
      <div className="absolute inset-x-2.5 bottom-8 z-[2] flex flex-col gap-2">
        <div role="status" aria-live="polite" className="flex flex-col items-center gap-1">
          {message ? (
            <p className="rounded-md bg-foreground/85 px-3 py-2 text-sm text-background shadow">
              {t(`pin.locate.${message}`)}
            </p>
          ) : (
            <p className="rounded-md bg-background/90 px-3 py-1.5 text-center text-sm shadow">
              {!inside ? t('pin.outside') : found ? t('pin.fromSearch') : t('pin.hint')}
            </p>
          )}
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="icon-lg"
            aria-label={t('pin.locate.button')}
            disabled={locating}
            onClick={locate}
            className="size-12 bg-background shadow"
          >
            <LocateFixedIcon aria-hidden="true" className={locating ? 'animate-pulse' : undefined} />
          </Button>
          <Button size="lg" className="h-12 flex-1 text-base shadow" disabled={!inside} onClick={confirm}>
            {t('pin.next')}
          </Button>
        </div>
      </div>
    </div>
  )
}
