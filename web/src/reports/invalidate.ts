import type { QueryClient } from '@tanstack/react-query'
import { placeCardQueryKey } from '@/place/placeApi'
import { reviewQueueQueryKey } from './reportsApi'

/**
 * After a report, a hide, an unhide or a resolution: the place's card, the map's places and counts (a hidden
 * recommendation is not counted, and a place with none left disappears), the signatures and the review queue are
 * read again.
 */
export function invalidateAfterModeration(queryClient: QueryClient, place: string): void {
  void queryClient.invalidateQueries({ queryKey: placeCardQueryKey(place) })
  void queryClient.invalidateQueries({ queryKey: ['places'] })
  void queryClient.invalidateQueries({ queryKey: ['signatures'] })
  void queryClient.invalidateQueries({ queryKey: reviewQueueQueryKey })
}
