import { recommendationParam } from '@/lib/paths'

/**
 * The query string to keep when moving between the map and its cards (the city and the map's filters, A6), without the
 * share link's ?r=, which names a recommendation of one place only.
 */
export function mapSearch(search: string): string {
  const params = new URLSearchParams(search)
  params.delete(recommendationParam)
  const rest = params.toString()
  return rest === '' ? '' : `?${rest}`
}

/** The id of a recommendation's element on the card, for scrolling to the one a shared link names (?r=, T032). */
export function recommendationElementId(rec: string): string {
  return `rec-${rec}`
}
