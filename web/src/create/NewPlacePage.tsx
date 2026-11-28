// A new place with its first recommendation, /new (B1–B7, C2–C4, M2-Q3): first the crosshair (PinStep), then the
// place's name and the recommendation. The shortest path is crosshair → name → attitude → one sentence → save; nothing
// else needs touching (G2). The second step is a history entry of its own (the point in its state), so that the back
// button returns to the crosshair; the form keeps what was typed meanwhile. Saving opens the new place's card.

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowLeftIcon, MapPinIcon } from 'lucide-react'
import { lazy, Suspense, type SyntheticEvent, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'
import { Button } from '@/components/ui/button'
import { PageHeading } from '@/components/PageHeading'
import { useMapSearch } from '@/filters/useFilters'
import { newPlacePath, placePath, recommendPath } from '@/lib/paths'
import { cityParam } from '@/map/rememberedCity'
import { createPlace, invalidateAfterWrite } from './createApi'
import { FormMessage, OfflineNotice } from './FormMessage'
import { focusFirstProblem, useCanUsePseudonym, useOnline } from './formUtils'
import type { PinView } from './PinStep'
import { emptyPlace, newPlaceProblems, type PlaceValues, toCreatePlaceRequest } from './placeForm'
import { PlaceFields } from './PlaceFields'
import { RecommendationFields, SaveBar } from './RecommendationFields'
import { emptyRecommendation, type Problems, type RecommendationValues, serverProblems } from './recommendationForm'

const PinStep = lazy(() => import('./PinStep'))

/** The state of the second step's history entry: where the crosshair was. */
interface DetailsState {
  pin: PinView
}

function isPinView(value: unknown): value is PinView {
  if (typeof value !== 'object' || value === null) {
    return false
  }
  const v = value as Partial<PinView>
  const p = v.point as Record<string, unknown> | undefined
  return (
    typeof v.city === 'string' &&
    Array.isArray(v.center) &&
    typeof v.zoom === 'number' &&
    p !== undefined &&
    typeof p.lat === 'number' &&
    typeof p.lon === 'number' &&
    (p.source === 'crosshair' || p.source === 'search')
  )
}

function pinOf(state: unknown): PinView | null {
  const pin = (state as Partial<DetailsState> | null)?.pin
  return isPinView(pin) ? pin : null
}

/** The recommendation typed so far, carried to /p/:place/recommend when the member picks a place of the hint. */
export interface RecommendDraftState {
  draft: RecommendationValues
}

export function NewPlacePage() {
  const { t } = useTranslation('create')
  const location = useLocation()
  const navigate = useNavigate()
  const pin = pinOf(location.state)
  // The crosshair's last view, to start there again after going back from the second step.
  const [lastPin, setLastPin] = useState<PinView | null>(pin)
  const [place, setPlace] = useState<PlaceValues>(emptyPlace)
  const [recommendation, setRecommendation] = useState<RecommendationValues>(emptyRecommendation)

  if (!pin) {
    return (
      <>
        <h1 className="sr-only">{t('newPlace.title')}</h1>
        <div className="h-[calc(100dvh-6.5rem-2px-env(safe-area-inset-top)-env(safe-area-inset-bottom))]">
          <Suspense
            fallback={
              <div role="status" className="flex h-full items-center justify-center text-sm">
                {t('pin.loading')}
              </div>
            }
          >
            <PinStep
              start={lastPin}
              onConfirm={(view) => {
                setLastPin(view)
                void navigate(newPlacePath, { state: { pin: view } satisfies DetailsState })
              }}
            />
          </Suspense>
        </div>
      </>
    )
  }
  return (
    <DetailsStep
      pin={pin}
      place={place}
      onPlace={setPlace}
      recommendation={recommendation}
      onRecommendation={setRecommendation}
      onBack={() => {
        void navigate(-1)
      }}
    />
  )
}

function DetailsStep({
  pin,
  place,
  onPlace,
  recommendation,
  onRecommendation,
  onBack,
}: {
  pin: PinView
  place: PlaceValues
  onPlace: (value: PlaceValues) => void
  recommendation: RecommendationValues
  onRecommendation: (value: RecommendationValues) => void
  onBack: () => void
}) {
  const { t } = useTranslation('create')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const online = useOnline()
  const mapSearch = useMapSearch()
  const canUsePseudonym = useCanUsePseudonym()
  const formRef = useRef<HTMLFormElement>(null)
  const [problems, setProblems] = useState<Problems>({})

  const save = useMutation({
    mutationFn: () => createPlace(toCreatePlaceRequest(place, recommendation, pin.point)),
    onSuccess: async (created) => {
      await invalidateAfterWrite(queryClient, created.place)
      // The card of the new place, on the map of its city, with the map's filters (T028, T029).
      const params = new URLSearchParams(mapSearch)
      params.set(cityParam, pin.city)
      void navigate({ pathname: placePath(created.place), search: `?${params.toString()}` }, { replace: true })
    },
    onError: () => {
      focusFirstProblem(formRef.current)
    },
  })
  const shown = { ...(serverProblems(save.error, 'recommendation.') ?? {}), ...problems }

  const submit = (e: SyntheticEvent) => {
    e.preventDefault()
    const found = newPlaceProblems(place, recommendation, { canUsePseudonym })
    setProblems(found)
    if (Object.keys(found).length > 0) {
      focusFirstProblem(formRef.current)
      return
    }
    save.mutate()
  }

  return (
    <div className="mx-auto flex max-w-lg flex-col">
      <PageHeading>{t('newPlace.title')}</PageHeading>
      <form ref={formRef} onSubmit={submit} noValidate className="flex flex-col gap-6 px-4">
        <OfflineNotice />
        <div className="flex items-center justify-between gap-2 rounded-lg bg-muted px-3 py-2 text-sm">
          <span className="flex items-center gap-2">
            <MapPinIcon aria-hidden="true" className="size-4 shrink-0" />
            {pin.point.source === 'search' ? t('newPlace.positionFromSearch') : t('newPlace.positionSet')}
          </span>
          <Button type="button" variant="ghost" size="sm" onClick={onBack}>
            <ArrowLeftIcon aria-hidden="true" />
            {t('newPlace.adjust')}
          </Button>
        </div>
        <PlaceFields
          value={place}
          onChange={onPlace}
          problems={shown}
          point={pin.point}
          onChooseNearby={(p) => {
            void navigate(recommendPath(p.uuid), { state: { draft: recommendation } satisfies RecommendDraftState })
          }}
        />
        <section aria-labelledby="new-place-recommendation" className="flex flex-col gap-4">
          <h2 id="new-place-recommendation" className="text-lg font-medium">
            {t('recommendation.title')}
          </h2>
          <RecommendationFields value={recommendation} onChange={onRecommendation} problems={shown} />
        </section>
        <SaveBar>
          <FormMessage problems={shown} error={save.error} />
          <Button type="submit" size="lg" className="h-12 w-full text-base" disabled={save.isPending || !online}>
            {save.isPending ? t('saving') : t('newPlace.save')}
          </Button>
        </SaveBar>
      </form>
    </div>
  )
}
