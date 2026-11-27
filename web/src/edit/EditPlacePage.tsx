import { useTranslation } from 'react-i18next'
import { ComingSoonPage, InvalidLinkPage } from '@/components/ComingSoon'
import { usePlaceParams } from '@/lib/paths'

/** Editing a place's fields, /p/:place/edit. Placeholder until T031. */
export function EditPlacePage() {
  const { t } = useTranslation('edit')
  const params = usePlaceParams()
  if (!params) {
    return <InvalidLinkPage />
  }
  return <ComingSoonPage title={t('place.title')} />
}
