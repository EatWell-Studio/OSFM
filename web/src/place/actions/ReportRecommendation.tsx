import type { RecommendationSlotProps } from './types'

/**
 * "Report" on one recommendation (T036): for every member who sees it except its author (recommendation.isMine).
 * Renders nothing until T036.
 */
// eslint-disable-next-line @typescript-eslint/no-unused-vars -- the props are the slot's contract for T036
export function ReportRecommendation(_props: RecommendationSlotProps) {
  return null
}
