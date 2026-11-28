import type { PlaceSlotProps, RecommendationSlotProps } from './types'

// Navigation and sharing (T032). Each recommendation of the card is an element with the id
// recommendationElementId(recommendation.uuid) (../links.ts), for scrolling to the one a shared link names (?r=).

/** "Navigate" (to the navigation apps) and "share to the group" for the whole place. Renders nothing until T032. */
// eslint-disable-next-line @typescript-eslint/no-unused-vars -- the props are the slot's contract for T032
export function PlaceNavigateShare(_props: PlaceSlotProps) {
  return null
}

/** "Share to the group" for one recommendation (shareLink(origin, place, rec)). Renders nothing until T032. */
// eslint-disable-next-line @typescript-eslint/no-unused-vars -- as above
export function RecommendationShare(_props: RecommendationSlotProps) {
  return null
}
