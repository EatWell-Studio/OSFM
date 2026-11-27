import { useTranslation } from 'react-i18next'
import { ComingSoonPage, InvalidLinkPage } from '@/components/ComingSoon'
import { usePlaceParams } from '@/lib/paths'

/** A place's card at its own address, /p/:place, which is also the share link (M2-Q15). Placeholder until T029. */
export function PlacePage() {
  const { t } = useTranslation('place')
  const params = usePlaceParams()
  if (!params) {
    return <InvalidLinkPage />
  }
  return <ComingSoonPage title={t('title')} />
}
