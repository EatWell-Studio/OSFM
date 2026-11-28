import { PlusIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { newPlacePath } from '@/lib/paths'

/**
 * The map's "+" (B1): a new place at /new, whose crosshair starts where the map is (its last view, T028) unless the
 * member's position is known.
 */
export function NewPlaceButton() {
  const { t } = useTranslation('create')
  return (
    <Button asChild size="icon-lg" className="size-12 rounded-full shadow-md">
      <Link to={newPlacePath} aria-label={t('newPlace.button')}>
        <PlusIcon aria-hidden="true" className="size-6" />
      </Link>
    </Button>
  )
}
