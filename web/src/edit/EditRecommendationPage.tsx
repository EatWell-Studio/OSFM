import { useTranslation } from 'react-i18next'
import { ComingSoonPage, InvalidLinkPage } from '@/components/ComingSoon'
import { usePlaceParams } from '@/lib/paths'

/** Editing a recommendation, /p/:place/r/:rec/edit. Placeholder until T031. */
export function EditRecommendationPage() {
  const { t } = useTranslation('edit')
  const params = usePlaceParams()
  if (!params) {
    return <InvalidLinkPage />
  }
  return <ComingSoonPage title={t('recommendation.title')} />
}
