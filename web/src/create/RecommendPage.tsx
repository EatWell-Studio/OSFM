// Adding one's recommendation to a place that exists, /p/:place/recommend (C1, M2-Q1): the recommendation form of a new
// place, alone. A member has at most one live recommendation per place: when they have one already (the card says so,
// or the server answers 409 already_recommended), the page leads to editing it instead. Saving opens the place's card.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useNavigate } from 'react-router'
import { InvalidLinkPage } from '@/components/ComingSoon'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { useMapSearch } from '@/filters/useFilters'
import { editRecommendationPath, placePath, usePlaceParams } from '@/lib/paths'
import { cityParam } from '@/map/rememberedCity'
import { isPlaceNotFound, type PlaceCard, placeCardQuery } from '@/place/placeApi'
import { addRecommendation, invalidateAfterWrite, WriteError } from './createApi'
import { OfflineNotice } from './FormMessage'
import { useOnline } from './formUtils'
import { RecommendationForm } from './RecommendationFields'
import { emptyRecommendation, type RecommendationValues } from './recommendationForm'

export function RecommendPage() {
  const params = usePlaceParams()
  if (!params) {
    return <InvalidLinkPage />
  }
  return <Recommend place={params.place} />
}

/** The draft a page handed over (the new place's form, when the member picked an existing place instead). */
function draftOf(state: unknown): RecommendationValues | null {
  const draft = (state as { draft?: unknown } | null)?.draft
  if (typeof draft !== 'object' || draft === null) {
    return null
  }
  const d = draft as Partial<RecommendationValues>
  return typeof d.text === 'string' && Array.isArray(d.sources) && Array.isArray(d.dishes) && Array.isArray(d.timeSlots)
    ? { ...emptyRecommendation, ...d }
    : null
}

function Recommend({ place }: { place: string }) {
  const { t } = useTranslation(['create', 'common'])
  const card = useQuery(placeCardQuery(place))

  let body
  if (card.isPending) {
    body = (
      <p role="status" className="text-muted-foreground">
        {t('common:app.loading')}
      </p>
    )
  } else if (card.isError) {
    body = isPlaceNotFound(card.error) ? (
      <p role="alert" className="text-muted-foreground">
        {t('errors.notFound')}
      </p>
    ) : (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p>{t('recommend.loadError')}</p>
        <Button
          variant="outline"
          onClick={() => {
            void card.refetch()
          }}
        >
          {t('common:retry')}
        </Button>
      </div>
    )
  } else {
    const mine = card.data.recommendations.find((r) => r.isMine)
    body = mine ? <AlreadyRecommended place={place} recommendation={mine.uuid} /> : <Form card={card.data} />
  }

  return (
    <div className="mx-auto flex max-w-lg flex-col">
      <PageHeading>{t('recommend.title')}</PageHeading>
      <div className="flex flex-col gap-4 px-4">
        {card.data && <p className="text-base font-medium break-words">{card.data.name}</p>}
        {body}
      </div>
    </div>
  )
}

/** The member's recommendation of this place exists: one per member and place, edited rather than added (M2-Q1). */
function AlreadyRecommended({ place, recommendation }: { place: string; recommendation: string }) {
  const { t } = useTranslation('create')
  return (
    <div role="alert" className="flex flex-col items-start gap-3">
      <p>{t('recommend.already')}</p>
      <Button asChild>
        <Link to={editRecommendationPath(place, recommendation)}>{t('recommend.editMine')}</Link>
      </Button>
      <Link to={placePath(place)} className="text-sm text-primary underline underline-offset-2">
        {t('recommend.backToPlace')}
      </Link>
    </div>
  )
}

function Form({ card }: { card: PlaceCard }) {
  const { t } = useTranslation('create')
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const online = useOnline()
  const mapSearch = useMapSearch()
  const initial = draftOf(location.state) ?? emptyRecommendation

  const save = useMutation({
    mutationFn: (input: Parameters<typeof addRecommendation>[1]) => addRecommendation(card.uuid, input),
    onSuccess: async () => {
      await invalidateAfterWrite(queryClient, card.uuid)
      const params = new URLSearchParams(mapSearch)
      if (card.city !== '') {
        params.set(cityParam, card.city)
      }
      void navigate({ pathname: placePath(card.uuid), search: `?${params.toString()}` }, { replace: true })
    },
  })

  if (save.error instanceof WriteError && save.error.code === 'already_recommended' && save.error.mine) {
    return <AlreadyRecommended place={card.uuid} recommendation={save.error.mine} />
  }
  return (
    <>
      <OfflineNotice />
      <RecommendationForm
        initial={initial}
        onSubmit={(input) => {
          save.mutate(input)
        }}
        saving={save.isPending}
        error={save.error}
        submitLabel={t('recommend.save')}
        canSave={online}
      />
    </>
  )
}
