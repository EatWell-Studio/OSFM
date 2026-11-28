import { PlusIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { recommendPath } from '@/lib/paths'
import type { PlaceSlotProps } from './types'

/**
 * "Add my recommendation" on the card, above the recommendations (T030): leads to recommendPath(place.uuid). Hidden
 * when one of the recommendations the card lists is the member's own (isMine; their own are always listed to them,
 * hidden and private ones included): a member has one live recommendation per place (M2-Q1), and editing it is on
 * the recommendation itself (T031).
 */
export function AddRecommendation({ place }: PlaceSlotProps) {
  const { t } = useTranslation('create')
  if (place.recommendations.some((r) => r.isMine)) {
    return null
  }
  return (
    <Button asChild variant="outline" className="h-10 self-start">
      <Link to={recommendPath(place.uuid)}>
        <PlusIcon aria-hidden="true" />
        {t('add.button')}
      </Link>
    </Button>
  )
}
