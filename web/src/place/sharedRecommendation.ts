// The recommendation a shared link names (?r=<uuid>, M2-Q15): the card scrolls to it once and highlights it while the
// address names it. A uuid that is not among the recommendations the member sees (deleted, private, hidden or mistyped)
// names nothing: the card opens as usual.

import { useEffect, useRef } from 'react'
import { useSearchParams } from 'react-router'
import { recommendationParam } from '@/lib/paths'
import { recommendationElementId } from './links'
import type { PlaceCard } from './placeApi'

/** The uuid of the recommendation of the card that the address names, or null. */
export function useSharedRecommendation(place: PlaceCard): string | null {
  const [params] = useSearchParams()
  const named = params.get(recommendationParam)
  const shared = named !== null && place.recommendations.some((r) => r.uuid === named) ? named : null

  // Once per place and recommendation: later re-renders (the card reloading, the city being set) leave the scrolling to
  // the member.
  const scrolled = useRef<string | null>(null)
  useEffect(() => {
    const key = shared === null ? null : `${place.uuid}/${shared}`
    if (key === null || scrolled.current === key) {
      return
    }
    scrolled.current = key
    document.getElementById(recommendationElementId(shared ?? ''))?.scrollIntoView({ block: 'start' })
  }, [place.uuid, shared])

  return shared
}
