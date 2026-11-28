import type { RecommendationSlotProps } from './types'

/**
 * Edit, delete and revision history of one recommendation (T031): editing for its author (recommendation.isMine) and
 * maintainers, deleting for its author only, at editRecommendationPath and revisionsPath. Renders nothing until T031.
 * The card already links "edited by a maintainer on …" to revisionsPath.
 */
// eslint-disable-next-line @typescript-eslint/no-unused-vars -- the props are the slot's contract for T031
export function RecommendationActions(_props: RecommendationSlotProps) {
  return null
}
