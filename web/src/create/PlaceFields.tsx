// The place's fields of a new place (B2–B6): the name, with the duplicate hint under it ("is it this one?", B6), and,
// closed at first, precision, category, cuisine, position note and landmark. Distances are rounded by the server
// (10 m) and coordinates never shown (H3).

import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { noFilters } from '@/filters/filters'
import { instanceQuery } from '@/instance/instance'
import { categories, cuisines, precisions } from '@/lib/enums'
import { byDistance } from '@/list/byDistance'
import { roundedDistance } from '@/map/places/geo'
import { placesQuery } from '@/map/places/placesApi'
import { CheckChips, Disclosure, RadioButtons, TextAreaField, TextField } from './controls'
import { selectClass, useProblemText } from './formUtils'
import { type NearbyPlace, nearbyQuery } from './createApi'
import type { PlaceValues, Point } from './placeForm'
import { limits, type Problems } from './recommendationForm'

/** How long the name must rest before the duplicate hint asks the server. */
export const nearbyDebounceMillis = 400

/** How far around the point the landmark's places are offered, in metres. */
const landmarkRadius = 300

const detailFields = ['precision', 'category', 'cuisine', 'positionNote', 'landmark']

export function PlaceFields({
  value,
  onChange,
  problems,
  point,
  onChooseNearby,
}: {
  value: PlaceValues
  onChange: (value: PlaceValues) => void
  problems: Problems
  point: Point
  /** The member picked a place of the duplicate hint: add the recommendation there instead. */
  onChooseNearby: (place: NearbyPlace) => void
}) {
  const { t } = useTranslation(['create', 'common'])
  const problemText = useProblemText()
  const set = (patch: Partial<PlaceValues>) => {
    onChange({ ...value, ...patch })
  }
  // The name once typing has paused, or the field was left.
  const [settled, setSettled] = useState('')
  const name = value.name.trim()
  useEffect(() => {
    const timer = setTimeout(() => {
      setSettled(name)
    }, nearbyDebounceMillis)
    return () => {
      clearTimeout(timer)
    }
  }, [name])

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-3">
        <TextField
          label={t('place.name')}
          value={value.name}
          onChange={(n) => {
            set({ name: n })
          }}
          onBlur={() => {
            setSettled(name)
          }}
          problem={problemText(problems.name, limits.name)}
          maxLength={limits.name}
          autoComplete="off"
          enterKeyHint="next"
        />
        {settled !== '' && settled.length <= limits.name && (
          <NearbyHint point={point} name={settled} onChoose={onChooseNearby} />
        )}
      </div>

      <Disclosure title={t('place.details')} invalid={Object.keys(problems).some((k) => detailFields.includes(k))}>
        <RadioButtons
          legend={t('place.precision')}
          name="precision"
          values={precisions}
          value={value.precision}
          label={(p) => t(`common:precision.${p}`)}
          onChange={(precision) => {
            set({ precision })
          }}
          problem={problemText(problems.precision)}
          hint={<p className="text-xs text-muted-foreground">{t('place.precisionHint')}</p>}
        />
        <div className="flex flex-col gap-1">
          <label htmlFor="new-place-category" className="text-sm font-medium">
            {t('place.category')}
          </label>
          <select
            id="new-place-category"
            value={value.category}
            aria-invalid={problems.category ? true : undefined}
            onChange={(e) => {
              set({ category: categories.find((c) => c === e.target.value) ?? '' })
            }}
            className={selectClass}
          >
            <option value="">{t('place.noCategory')}</option>
            {categories.map((c) => (
              <option key={c} value={c}>
                {t(`common:category.${c}`)}
              </option>
            ))}
          </select>
        </div>
        <CheckChips
          legend={t('place.cuisine')}
          values={cuisines}
          chosen={value.cuisine}
          label={(c) => t(`common:cuisine.${c}`)}
          onChange={(cuisine) => {
            set({ cuisine })
          }}
          problem={problemText(problems.cuisine)}
        />
        <TextAreaField
          label={t('place.positionNote')}
          hint={t('place.positionNoteHint')}
          value={value.positionNote}
          onChange={(positionNote) => {
            set({ positionNote })
          }}
          problem={problemText(problems.positionNote, limits.positionNote)}
          maxLength={limits.positionNote}
        />
        <Landmark
          value={value}
          onChange={onChange}
          point={point}
          problem={problemText(problems.landmark, limits.landmark)}
        />
      </Disclosure>
    </div>
  )
}

/** "Is it this one?": the places nearby with a similar name, which the member can pick instead of a duplicate (B6). */
function NearbyHint({ point, name, onChoose }: { point: Point; name: string; onChoose: (place: NearbyPlace) => void }) {
  const { t } = useTranslation(['create', 'common', 'map'])
  const nearby = useQuery(nearbyQuery(point, name))
  const places = nearby.data ?? []
  return (
    <div role="status" aria-live="polite">
      {places.length > 0 && (
        <section
          aria-labelledby="nearby-title"
          className="flex flex-col gap-2 rounded-lg border border-amber-500/60 bg-amber-50 p-3 dark:bg-amber-950/40"
        >
          <h2 id="nearby-title" className="text-sm font-medium">
            {t('place.nearbyTitle')}
          </h2>
          <p className="text-xs text-muted-foreground">{t('place.nearbyHint')}</p>
          <ul className="flex flex-col gap-2">
            {places.map((p) => (
              <li key={p.uuid}>
                <Button
                  type="button"
                  variant="outline"
                  className="h-auto min-h-11 w-full justify-between gap-2 py-2 text-left whitespace-normal"
                  onClick={() => {
                    onChoose(p)
                  }}
                >
                  <span className="flex min-w-0 flex-col">
                    <span className="font-medium break-words">{p.name}</span>
                    <span className="text-xs text-muted-foreground">
                      {[
                        (categories as readonly string[]).includes(p.category)
                          ? t(`common:category.${p.category as (typeof categories)[number]}`)
                          : null,
                        t('map:list.metres', { value: p.distance }),
                      ]
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  </span>
                  <span className="shrink-0 text-sm text-primary">{t('place.nearbyChoose')}</span>
                </Button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}

/** The landmark (B4): none, one of the places around the point, or a text. */
function Landmark({
  value,
  onChange,
  point,
  problem,
}: {
  value: PlaceValues
  onChange: (value: PlaceValues) => void
  point: Point
  problem: string | null
}) {
  const { t } = useTranslation(['create', 'map'])
  const instance = useQuery(instanceQuery)
  const city = instance.data?.cities.find(
    (c) => point.lon >= c.bbox[0] && point.lon <= c.bbox[2] && point.lat >= c.bbox[1] && point.lat <= c.bbox[3],
  )
  // A box of about landmarkRadius around the point, rounded so that small moves keep the same query.
  const box = useMemo(() => {
    const dLat = landmarkRadius / 111_320
    const dLon = dLat / Math.max(0.1, Math.cos((point.lat * Math.PI) / 180))
    const r = (n: number) => Math.round(n * 1000) / 1000
    return [r(point.lon - dLon), r(point.lat - dLat), r(point.lon + dLon), r(point.lat + dLat)] as [
      number,
      number,
      number,
      number,
    ]
  }, [point.lat, point.lon])
  const places = useQuery({
    ...placesQuery(city?.slug ?? '', box, noFilters),
    enabled: city !== undefined && value.landmark.type === 'place',
  })
  const nearby = useMemo(
    () =>
      byDistance(places.data ?? [], point)
        .filter((p) => p.distance <= landmarkRadius)
        .slice(0, 20),
    [places.data, point],
  )
  const kind = value.landmark.type
  const setKind = (type: PlaceValues['landmark']['type']) => {
    onChange({
      ...value,
      landmark: type === 'none' ? { type } : type === 'place' ? { type, place: '' } : { type, text: '' },
    })
  }

  return (
    <div className="flex flex-col gap-3">
      <RadioButtons
        legend={t('place.landmark')}
        name="landmark"
        values={['none', 'place', 'text'] as const}
        value={kind}
        label={(k) => t(`place.landmarkKind.${k}`)}
        onChange={setKind}
      />
      {value.landmark.type === 'place' && (
        <div className="flex flex-col gap-1">
          <label htmlFor="new-place-landmark-place" className="text-sm font-medium">
            {t('place.landmarkPlace')}
          </label>
          {places.isSuccess && nearby.length === 0 ? (
            <p className="text-sm text-muted-foreground">{t('place.noLandmarkPlaces')}</p>
          ) : (
            <select
              id="new-place-landmark-place"
              value={value.landmark.place}
              aria-invalid={problem ? true : undefined}
              onChange={(e) => {
                onChange({ ...value, landmark: { type: 'place', place: e.target.value } })
              }}
              className={selectClass}
            >
              <option value="">{t('place.landmarkChoose')}</option>
              {nearby.map(({ place, distance }) => {
                const { unit, value: d } = roundedDistance(distance)
                return (
                  <option key={place.uuid} value={place.uuid}>
                    {`${place.name} · ${unit === 'm' ? t('map:list.metres', { value: d }) : t('map:list.kilometres', { value: d })}`}
                  </option>
                )
              })}
            </select>
          )}
          {problem && <p className="text-sm text-destructive">{problem}</p>}
        </div>
      )}
      {value.landmark.type === 'text' && (
        <TextField
          label={t('place.landmarkText')}
          hint={t('place.landmarkTextHint')}
          value={value.landmark.text}
          onChange={(text) => {
            onChange({ ...value, landmark: { type: 'text', text } })
          }}
          problem={problem}
          maxLength={limits.landmark}
        />
      )}
    </div>
  )
}
