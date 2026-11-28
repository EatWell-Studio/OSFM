/** The id of a recommendation's element on the card, for scrolling to the one a shared link names (?r=, T032). */
export function recommendationElementId(rec: string): string {
  return `rec-${rec}`
}
