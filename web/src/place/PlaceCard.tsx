// The contents of a place's card (T029): the place, its counts and the recommendations the member may see, as the
// server sent them (GET /api/osfm/places/{uuid}). Every text a member wrote is plain text (white-space keeps its line
// breaks); nothing is ever rendered as HTML or Markdown. Coordinates are never shown (H3). Source links open in a new
// window and are not fetched before the member clicks one.
//
// The actions of other tasks are slots in ./actions, one file per task; this file only places them.

import type { ReactNode, Ref } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { AttitudeBadge } from '@/components/AttitudeBadge'
import { SignatureLinks } from '@/components/SignatureLinks'
import { SignatureName } from '@/components/SignatureName'
import { TimeSlotChips } from '@/components/TimeSlotChips'
import { Badge } from '@/components/ui/badge'
import { useMapSearch } from '@/filters/useFilters'
import type { PlaceCounts, Source } from '@/api/types.gen'
import { type Attitude, attitudes } from '@/lib/enums'
import { placePath, revisionsPath } from '@/lib/paths'
import { AddRecommendation } from './actions/AddRecommendation'
import { PlaceNavigateShare, RecommendationShare } from './actions/NavigateShare'
import { RecommendationActions } from './actions/RecommendationActions'
import { ReportRecommendation } from './actions/ReportRecommendation'
import {
  formatDate,
  formatDay,
  isAttitude,
  isCategory,
  isCuisine,
  isHiddenKind,
  isPlaceStatus,
  isSourceKind,
  sourceLink,
} from './format'
import { recommendationElementId } from './links'
import type { PlaceCard as Card, RecommendationView } from './placeApi'

const countFields: Record<Attitude, keyof PlaceCounts> = {
  must_try: 'mustTry',
  good: 'good',
  ok: 'ok',
  avoid: 'avoid',
}

/**
 * The card. The heading is the place's name; it takes the focus when the card opens (headingRef), so that screen
 * readers start there.
 */
export function PlaceCard({
  place,
  headingId = 'place-card',
  headingRef,
}: {
  place: Card
  headingId?: string
  headingRef?: Ref<HTMLHeadingElement>
}) {
  const { t } = useTranslation(['place', 'common'])
  // The city and filters of the map, kept on the way to another card; not the share link's ?r= (T028).
  const search = useMapSearch()
  const category = isCategory(place.category) ? place.category : null
  const cuisines = place.cuisine.filter(isCuisine)
  const approximate = place.precision === 'approx' || place.precision === 'area'

  return (
    <article className="flex flex-col gap-4">
      <header className="flex flex-col gap-1 pr-10">
        <h2 id={headingId} ref={headingRef} tabIndex={-1} className="text-xl font-semibold break-words outline-none">
          {place.name}
        </h2>
        {place.aliases.length > 0 && (
          <p className="text-sm text-muted-foreground break-words">
            {t('aliases', { aliases: place.aliases.join(t('listSeparator')) })}
          </p>
        )}
        {(category !== null || cuisines.length > 0) && (
          <div className="flex flex-wrap items-center gap-1 text-sm">
            {category !== null && <span>{t(`common:category.${category}`)}</span>}
            {cuisines.map((c) => (
              <Badge key={c} variant="outline">
                {t(`common:cuisine.${c}`)}
              </Badge>
            ))}
          </div>
        )}
        <Counts counts={place.counts} />
        {isPlaceStatus(place.status) && (
          <p className="text-sm">
            {t(`status.${place.status}`)}
            {place.statusConfirmedAt && (
              <span className="text-muted-foreground">
                {' · '}
                <Day moment={place.statusConfirmedAt} label="statusConfirmed" />
              </span>
            )}
          </p>
        )}
      </header>

      <PlaceNavigateShare place={place} />

      {approximate && (
        <p className="text-sm font-medium">{t(place.precision === 'area' ? 'approximateArea' : 'approximate')}</p>
      )}

      {(place.positionNote !== '' || place.landmark) && (
        <dl className="flex flex-col gap-2 text-sm">
          {place.positionNote !== '' && (
            <div>
              <dt className="text-muted-foreground">{t('positionNote')}</dt>
              <dd className="break-words whitespace-pre-line">{place.positionNote}</dd>
            </div>
          )}
          {place.landmark?.type === 'place' && place.landmark.place && (
            <div>
              <dt className="text-muted-foreground">{t('landmark')}</dt>
              <dd>
                <Link
                  to={{ pathname: placePath(place.landmark.place.uuid), search }}
                  className="break-words text-primary underline underline-offset-2"
                >
                  {place.landmark.place.name}
                </Link>
              </dd>
            </div>
          )}
          {place.landmark?.type === 'text' && place.landmark.text && (
            <div>
              <dt className="text-muted-foreground">{t('landmark')}</dt>
              <dd className="break-words whitespace-pre-line">{place.landmark.text}</dd>
            </div>
          )}
        </dl>
      )}

      <section aria-labelledby={`${headingId}-recs`} className="flex flex-col gap-3">
        <h3 id={`${headingId}-recs`} className="text-base font-medium">
          {t('recommendations')}
        </h3>
        <AddRecommendation place={place} />
        <ol className="flex flex-col gap-3">
          {place.recommendations.map((rec) => (
            <Recommendation key={rec.uuid} place={place} rec={rec} />
          ))}
        </ol>
      </section>
    </article>
  )
}

/** "Must try 2 · Good 1 · Avoid 1": the numbers of people per attitude, leaving out zeros; never an average (A4). */
function Counts({ counts }: { counts: PlaceCounts }) {
  const { t } = useTranslation(['place', 'common'])
  const shown = attitudes.filter((a) => counts[countFields[a]] > 0)
  if (shown.length === 0) {
    return null
  }
  return (
    <p aria-label={t('counts')} className="text-sm font-medium">
      {shown.map((a) => t('count', { attitude: t(`common:attitude.${a}`), count: counts[countFields[a]] })).join(' · ')}
    </p>
  )
}

/** A date in the member's language, inside one of the place namespace's texts that take {{date}}. */
function Day({
  moment,
  day,
  label,
}: {
  moment?: string
  day?: string
  label: 'statusConfirmed' | 'visitedOn' | 'editedByMaintainer'
}) {
  const { t, i18n } = useTranslation('place')
  const date = day !== undefined ? formatDay(day, i18n.language) : formatDate(moment ?? '', i18n.language)
  return <>{t(label, { date })}</>
}

function Recommendation({ place, rec }: { place: Card; rec: RecommendationView }) {
  const { t } = useTranslation(['place', 'common'])
  const name = rec.authorLeft ? t('authorLeft') : rec.displayName
  return (
    <li
      id={recommendationElementId(rec.uuid)}
      data-hidden={rec.hidden ? 'true' : undefined}
      className={'flex flex-col gap-2 rounded-lg border p-3 text-sm' + (rec.hidden ? ' border-dashed bg-muted/50' : '')}
    >
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <SignatureName name={name} />
        {isAttitude(rec.attitude) && <AttitudeBadge attitude={rec.attitude} />}
        {rec.visibility === 'private' && <Badge variant="outline">{t('common:visibility.private')}</Badge>}
        {rec.hidden && <Badge variant="destructive">{t('hidden')}</Badge>}
      </div>
      {/* Only a main-name signature carries links (the server sends none with a pseudonym, E3). */}
      <SignatureLinks links={rec.externalLinks} name={name} />
      {rec.hidden && <HiddenNote kind={rec.hidden.kind} reason={rec.hidden.reason} />}
      <TimeSlotChips slots={rec.timeSlots} label={t('timeSlots')} />
      {rec.season !== '' && <p className="break-words">{t('season', { season: rec.season })}</p>}
      {rec.dishes.length > 0 && (
        <div className="flex flex-wrap items-center gap-1">
          <span className="text-muted-foreground">{t('dishes')}</span>
          <ul aria-label={t('dishes')} className="contents">
            {rec.dishes.map((dish, i) => (
              <li key={i}>
                <Badge variant="secondary" className="h-auto whitespace-normal break-words">
                  {dish}
                </Badge>
              </li>
            ))}
          </ul>
        </div>
      )}
      <p className="break-words whitespace-pre-line">{rec.text}</p>
      <Sources sources={rec.sources} />
      {rec.visitedOn && (
        <p className="text-muted-foreground">
          <Day day={rec.visitedOn} label="visitedOn" />
        </p>
      )}
      {rec.editedByMaintainerAt && (
        <p>
          <Link to={revisionsPath(place.uuid, rec.uuid)} className="text-muted-foreground underline underline-offset-2">
            <Day moment={rec.editedByMaintainerAt} label="editedByMaintainer" />
          </Link>
        </p>
      )}
      <Actions>
        <RecommendationActions place={place} recommendation={rec} />
        <RecommendationShare place={place} recommendation={rec} />
        <ReportRecommendation place={place} recommendation={rec} />
      </Actions>
    </li>
  )
}

/** The row of a recommendation's action slots; it takes no space while every slot renders nothing. */
function Actions({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-2 empty:hidden">{children}</div>
}

/** Why a hidden recommendation is hidden: shown to its author and to maintainers, the only ones who see it (C6). */
function HiddenNote({ kind, reason }: { kind: string; reason?: string }) {
  const { t } = useTranslation('place')
  return (
    <div className="flex flex-col gap-1 text-muted-foreground">
      {isHiddenKind(kind) && <p>{t(`hiddenKind.${kind}`)}</p>}
      {reason && <p className="break-words whitespace-pre-line">{t('hiddenReason', { reason })}</p>}
    </div>
  )
}

/** A recommendation's sources: kind, link (new window, no referrer, never prefetched) and excerpt. */
function Sources({ sources }: { sources: Source[] }) {
  const { t } = useTranslation(['place', 'common'])
  if (sources.length === 0) {
    return null
  }
  return (
    <div className="flex flex-col gap-1">
      <span className="text-muted-foreground">{t('sources')}</span>
      <ul aria-label={t('sources')} className="flex flex-col gap-2">
        {sources.map((source, i) => {
          const link = sourceLink(source.url)
          return (
            <li key={i} className="flex flex-col gap-1 border-l-2 pl-2">
              <span className="flex flex-wrap items-center gap-x-2">
                {isSourceKind(source.kind) && <Badge variant="outline">{t(`common:sourceKind.${source.kind}`)}</Badge>}
                {link ? (
                  <a
                    href={link.href}
                    target="_blank"
                    rel="noopener noreferrer"
                    referrerPolicy="no-referrer"
                    className="break-all text-primary underline underline-offset-2"
                    translate="no"
                  >
                    {link.text}
                    <span className="sr-only">{t('newWindow')}</span>
                  </a>
                ) : (
                  source.url && <span className="break-all">{source.url}</span>
                )}
              </span>
              {source.excerpt && <q className="break-words whitespace-pre-line">{source.excerpt}</q>}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
