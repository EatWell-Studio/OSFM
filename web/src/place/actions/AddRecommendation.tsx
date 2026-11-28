import type { PlaceSlotProps } from './types'

/**
 * "Add my recommendation" on the card, above the recommendations (T030): leads to recommendPath(place.uuid), or to the
 * member's own recommendation when one of place.recommendations isMine. Renders nothing until T030.
 */
// eslint-disable-next-line @typescript-eslint/no-unused-vars -- the props are the slot's contract for T030
export function AddRecommendation(_props: PlaceSlotProps) {
  return null
}
