import { useTranslation } from 'react-i18next'
import { ComingSoonPage } from '@/components/ComingSoon'

/** A new place with its first recommendation, /new. Placeholder until T030. */
export function NewPlacePage() {
  const { t } = useTranslation('create')
  return <ComingSoonPage title={t('newPlace.title')} />
}
