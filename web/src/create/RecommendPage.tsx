import { useTranslation } from 'react-i18next'
import { ComingSoonPage, InvalidLinkPage } from '@/components/ComingSoon'
import { usePlaceParams } from '@/lib/paths'

/** Adding one's recommendation to a place that exists, /p/:place/recommend. Placeholder until T030. */
export function RecommendPage() {
  const { t } = useTranslation('create')
  const params = usePlaceParams()
  if (!params) {
    return <InvalidLinkPage />
  }
  return <ComingSoonPage title={t('recommend.title')} />
}
