// Navigation and sharing (T032). "Directions" opens the place in Amap, Apple Maps or Google Maps, each with the datum
// it expects (src/geo/navigation.ts); the links are followed only when tapped and are never prefetched. "Share with the
// group" hands a short text with the place's link (with ?r= for one recommendation) to the share sheet or the clipboard
// (../share.ts). A shared link names a recommendation with ?r=; the card scrolls to it and highlights it
// (../sharedRecommendation.ts).

import { useQuery } from '@tanstack/react-query'
import { Navigation, Share2 } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { navigationApps, navigationLink } from '@/geo/navigation'
import { instanceQuery } from '@/instance/instance'
import { cityLabel } from '@/map/cities'
import type { PlaceCard, RecommendationView } from '../placeApi'
import { type ShareOutcome, shareOrCopy, shareText } from '../share'
import type { PlaceSlotProps, RecommendationSlotProps } from './types'

/** "Directions" (to the navigation apps) and "share with the group" for the whole place. */
export function PlaceNavigateShare({ place }: PlaceSlotProps) {
  const { t } = useTranslation('share')
  const labelId = useId()
  return (
    <div className="flex flex-col gap-2">
      <div role="group" aria-labelledby={labelId} className="flex flex-wrap items-center gap-2">
        <span id={labelId} className="flex items-center gap-1 text-sm text-muted-foreground">
          <Navigation aria-hidden="true" className="size-4" />
          {t('navigate')}
        </span>
        {navigationApps.map((app) => (
          <Button key={app} variant="outline" size="sm" asChild>
            <a href={navigationLink(app, place)} target="_blank" rel="noopener noreferrer" referrerPolicy="no-referrer">
              {t(`app.${app}`)}
              <span className="sr-only">{t('newWindow')}</span>
            </a>
          </Button>
        ))}
      </div>
      <ShareButton place={place} label={t('sharePlace')} />
    </div>
  )
}

/**
 * "Share with the group" for one recommendation. Not on a recommendation that only its author (private) or its author
 * and the maintainers (hidden) can see: the link would show it to no one else.
 */
export function RecommendationShare({ place, recommendation }: RecommendationSlotProps) {
  const { t } = useTranslation(['share', 'place'])
  if (recommendation.visibility === 'private' || recommendation.hidden) {
    return null
  }
  const name = recommendation.authorLeft ? t('place:authorLeft') : recommendation.displayName
  return (
    <ShareButton
      place={place}
      recommendation={recommendation}
      label={t('shareRecommendation', { name })}
      size="sm"
      variant="ghost"
    />
  )
}

/** The display name of the place's city: the instance's name for it, else one made from its slug. */
function useCityName(city: string): string {
  const instance = useQuery(instanceQuery)
  if (city === '') {
    return ''
  }
  return instance.data?.cities.find((c) => c.slug === city)?.name ?? cityLabel({ id: city })
}

function ShareButton({
  place,
  recommendation,
  label,
  size = 'default',
  variant = 'outline',
}: {
  place: PlaceCard
  recommendation?: RecommendationView
  label: string
  size?: 'default' | 'sm'
  variant?: 'outline' | 'ghost'
}) {
  const { t } = useTranslation(['share', 'place', 'common'])
  const cityName = useCityName(place.city)
  const [outcome, setOutcome] = useState<{ result: ShareOutcome; text: string } | null>(null)
  const textId = useId()
  // Built before the tap: the share sheet must open within the tap's user activation, without waiting for anything.
  const text = shareText({ place, recommendation, cityName, origin: window.location.origin, t })

  return (
    <div className="flex flex-col items-start gap-2">
      <Button
        variant={variant}
        size={size}
        aria-label={label}
        onClick={() => {
          setOutcome(null)
          void shareOrCopy(text).then((result) => {
            setOutcome({ result, text })
          })
        }}
      >
        <Share2 aria-hidden="true" />
        {t('share')}
      </Button>
      <div role="status" className="text-sm empty:hidden">
        {outcome?.result === 'copied' && <p>{t('copied')}</p>}
      </div>
      {outcome?.result === 'failed' && (
        <div className="flex w-full flex-col gap-1 text-sm">
          <p id={textId} role="alert">
            {t('copyFailed')}
          </p>
          <textarea
            aria-labelledby={textId}
            readOnly
            value={outcome.text}
            rows={4}
            onFocus={(e) => {
              e.target.select()
            }}
            className="w-full rounded-lg border bg-background p-2 text-sm"
          />
        </div>
      )}
    </div>
  )
}
