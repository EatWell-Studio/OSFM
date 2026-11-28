// "Share with the group" (C7, M2-Q15): a short text about the place, or one of its recommendations, followed by its
// link. The text goes to the system's share sheet when the browser has one (Web Share API), else to the clipboard; the
// member then posts it in the group chat. What leaves the app is the member's own choice: nothing is sent before the
// tap, and nothing but this text.

import type { TFunction } from 'i18next'
import { shareLink } from '@/lib/paths'
import { isAttitude } from './format'
import type { PlaceCard, RecommendationView } from './placeApi'

/** How many characters of a recommendation's text the shared text quotes (M2-Q15). */
export const excerptLength = 60

const countFields = [
  ['must_try', 'mustTry'],
  ['good', 'good'],
  ['ok', 'ok'],
  ['avoid', 'avoid'],
] as const

/** One line: runs of white space (line breaks included) become a single space. */
function oneLine(text: string): string {
  return text.replace(/\s+/g, ' ').trim()
}

/** The first n characters as people count them (grapheme clusters, so that no emoji or accent is cut in two). */
function firstCharacters(text: string, n: number): { text: string; cut: boolean } {
  const characters =
    typeof Intl.Segmenter === 'function'
      ? Array.from(new Intl.Segmenter(undefined, { granularity: 'grapheme' }).segment(text), (s) => s.segment)
      : Array.from(text)
  return characters.length > n ? { text: characters.slice(0, n).join(''), cut: true } : { text, cut: false }
}

export interface ShareInput {
  place: PlaceCard
  /** One recommendation of the place, to share that one (its link has ?r=). */
  recommendation?: RecommendationView
  /** The display name of the place's city ('' when the place has none). */
  cityName: string
  /** The instance's origin, e.g. https://food.example.org. */
  origin: string
  t: TFunction<['share', 'place', 'common']>
}

/**
 * The text to share: "Name (City) · position note · Must try 2 · Good 1", for a recommendation a second line with its
 * attitude, signature and the start of its text, and the link on the last line. Member texts stay as they are (the
 * chat shows plain text); only line breaks become spaces.
 */
export function shareText({ place, recommendation, cityName, origin, t }: ShareInput): string {
  const parts = [cityName ? t('share:text.placeWithCity', { name: place.name, city: cityName }) : place.name]
  const note = oneLine(place.positionNote)
  if (note !== '') {
    parts.push(note)
  }
  for (const [attitude, field] of countFields) {
    const count = place.counts[field]
    if (count > 0) {
      parts.push(t('place:count', { attitude: t(`common:attitude.${attitude}`), count }))
    }
  }
  const lines = [parts.join(t('share:text.separator'))]
  if (recommendation) {
    const excerpt = firstCharacters(oneLine(recommendation.text), excerptLength)
    lines.push(
      t('share:text.recommendation', {
        attitude: isAttitude(recommendation.attitude) ? t(`common:attitude.${recommendation.attitude}`) : '',
        name: recommendation.authorLeft ? t('place:authorLeft') : recommendation.displayName,
        text: excerpt.cut ? excerpt.text + t('share:text.ellipsis') : excerpt.text,
      }),
    )
  }
  lines.push(shareLink(origin, place.uuid, recommendation?.uuid))
  return lines.join('\n')
}

export type ShareOutcome = 'shared' | 'copied' | 'cancelled' | 'failed'

/**
 * Hands the text to the system's share sheet when there is one, else copies it. A share sheet the member closes is
 * 'cancelled'; one the browser refuses falls back to the clipboard. 'failed' when neither worked: the caller shows the
 * text to copy by hand.
 */
export async function shareOrCopy(text: string): Promise<ShareOutcome> {
  // The link is part of the text rather than a separate url: some share targets keep only one of the two.
  const data: ShareData = { text }
  if (typeof navigator.share === 'function' && (typeof navigator.canShare !== 'function' || navigator.canShare(data))) {
    try {
      await navigator.share(data)
      return 'shared'
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') {
        return 'cancelled'
      }
    }
  }
  try {
    await navigator.clipboard.writeText(text)
    return 'copied'
  } catch {
    return 'failed'
  }
}
