// The props of the card's action slots (T029). Each slot is a component in a file of its own, owned by the task that
// builds it: that task replaces the file's body and leaves the card (PlaceCard.tsx) as it is. A slot renders nothing
// until then. Changes go through the API and then invalidate placeCardQueryKey(place.uuid) (placeApi.ts), which
// reloads the card.

import type { PlaceCard, RecommendationView } from '../placeApi'

/** A slot about the whole place: the card as the server sent it. */
export interface PlaceSlotProps {
  place: PlaceCard
}

/** A slot on one recommendation of the place: the card and the recommendation, as the server sent them. */
export interface RecommendationSlotProps {
  place: PlaceCard
  recommendation: RecommendationView
}
