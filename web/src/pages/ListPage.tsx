import { useQuery } from '@tanstack/react-query'
import { LocateFixedIcon, MapPinIcon } from 'lucide-react'
import { useCallback, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router'
import type { PlaceCounts } from '@/api/types.gen'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { FilterSheet } from '@/filters/FilterSheet'
import { filterCount } from '@/filters/filters'
import { useFilters, useRememberMapSearch } from '@/filters/useFilters'
import { instanceQuery } from '@/instance/instance'
import { byDistance } from '@/list/byDistance'
import { categories } from '@/lib/enums'
import { placePath } from '@/lib/paths'
import { pickCity } from '@/map/cities'
import { clipBox, roundedDistance } from '@/map/places/geo'
import { placesQuery } from '@/map/places/placesApi'
import { lastView } from '@/map/places/view'
import { cityParam, readRememberedCity } from '@/map/rememberedCity'
import { type Position, useLocate } from '@/map/useLocate'

/**
 * The places in the map's current view, /list (A6; docs/architecture.md §4.2, §4.3): the same city, area and filters
 * as the map, nearest first from the map's center, or from the member's own position when they ask for it (the
 * position stays in the page). Without a view of the map yet (the list opened first), the whole city, from its center.
 */
export function ListPage() {
  const { t } = useTranslation('map')
  const search = useRememberMapSearch()
  const instance = useQuery(instanceQuery)
  const [searchParams] = useSearchParams()
  const cities = instance.data?.cities ?? []
  const city = pickCity(
    cities.map((c) => ({ ...c, id: c.slug })),
    searchParams.get(cityParam),
    readRememberedCity(),
  )

  return (
    <>
      <PageHeading>{t('list.title')}</PageHeading>
      {instance.isPending ? (
        <p role="status" className="px-4 text-sm text-muted-foreground">
          {t('list.loading')}
        </p>
      ) : !city ? (
        <p className="px-4 text-sm text-muted-foreground">{t('list.noCity')}</p>
      ) : (
        <CityList
          key={city.slug}
          city={city.slug}
          bbox={city.bbox}
          center={city.center}
          cityName={city.name}
          search={search}
        />
      )}
    </>
  )
}

function CityList({
  city,
  cityName,
  bbox,
  center,
  search,
}: {
  city: string
  cityName: string
  bbox: [number, number, number, number]
  center: [number, number]
  search: string
}) {
  const { t } = useTranslation('map')
  const [filters] = useFilters()
  // Read once: the view does not change while the list is shown.
  const [view] = useState(() => lastView(city))
  const box = view ? clipBox(view.bounds, bbox) : null
  const places = useQuery(placesQuery(city, box, filters))

  const [position, setPosition] = useState<Position | null>(null)
  const onPosition = useCallback((p: Position) => {
    setPosition(p)
    return true
  }, [])
  const { locate, locating, message } = useLocate(onPosition)
  const [lon, lat] = position ? [position.lon, position.lat] : (view?.center ?? center)
  const sorted = useMemo(() => byDistance(places.data ?? [], { lon, lat }), [places.data, lon, lat])

  let basis: string
  if (position) {
    basis = t('list.fromMe')
  } else if (view) {
    basis = t('list.fromMapCenter')
  } else {
    basis = t('list.fromCityCenter', { city: cityName })
  }

  return (
    <div className="flex flex-col gap-3 px-4 pb-4">
      <div className="flex flex-wrap items-center gap-2">
        <FilterSheet city={city} />
        <Button
          variant="outline"
          disabled={locating}
          aria-pressed={position !== null}
          onClick={() => {
            if (position) {
              setPosition(null)
            } else {
              locate()
            }
          }}
        >
          <LocateFixedIcon aria-hidden="true" className={locating ? 'animate-pulse' : undefined} />
          {t('list.useMyPosition')}
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">{basis}</p>
      <div role="status" aria-live="polite">
        {locating && <p className="sr-only">{t('locate.locating')}</p>}
        {message && message !== 'outside' && <p className="text-sm">{t(`locate.${message}`)}</p>}
      </div>

      {places.isPending ? (
        <p role="status" className="text-sm text-muted-foreground">
          {t('list.loading')}
        </p>
      ) : places.isError ? (
        <div role="alert" className="flex flex-col items-start gap-2 text-sm">
          <p>{t('places.loadError')}</p>
          <Button
            variant="outline"
            onClick={() => {
              void places.refetch()
            }}
          >
            {t('retry')}
          </Button>
        </div>
      ) : sorted.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {filterCount(filters) > 0 ? t('places.noMatch') : t('list.empty')}
        </p>
      ) : (
        <ul aria-label={t('list.places')} className="flex flex-col divide-y rounded-md border">
          {sorted.map(({ place, distance }) => (
            <li key={place.uuid}>
              <Link
                to={placePath(place.uuid) + search}
                className="flex items-start gap-3 p-3 outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
              >
                <MapPinIcon aria-hidden="true" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  {/* The name is a member's text: plain text. */}
                  <span className="font-medium break-words">{place.name}</span>
                  <span className="text-sm text-muted-foreground">
                    <CategoryLabel category={place.category} />
                    <Counts counts={place.counts} />
                  </span>
                </span>
                <Distance metres={distance} />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function CategoryLabel({ category }: { category: string }) {
  const { t } = useTranslation()
  const known = categories.find((c) => c === category)
  return known ? <span className="mr-2">{t(`category.${known}`)}</span> : null
}

/** The number of people per attitude, as "must try 2 · good 1"; attitudes nobody chose are left out, no average. */
function Counts({ counts }: { counts: PlaceCounts }) {
  const { t } = useTranslation()
  const parts = (
    [
      ['must_try', counts.mustTry],
      ['good', counts.good],
      ['ok', counts.ok],
      ['avoid', counts.avoid],
    ] as const
  )
    .filter(([, n]) => n > 0)
    .map(([attitude, n]) => `${t(`attitude.${attitude}`)} ${String(n)}`)
  return parts.length > 0 ? <span>{parts.join(' · ')}</span> : null
}

/** The distance, rounded to 10 m or 0.1 km (H3). */
function Distance({ metres }: { metres: number }) {
  const { t } = useTranslation('map')
  const { unit, value } = roundedDistance(metres)
  return (
    <span className="shrink-0 text-sm text-muted-foreground tabular-nums">
      {unit === 'm' ? t('list.metres', { value }) : t('list.kilometres', { value })}
    </span>
  )
}
