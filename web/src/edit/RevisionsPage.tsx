import { useTranslation } from 'react-i18next'
import { ComingSoonPage, InvalidLinkPage } from '@/components/ComingSoon'
import { usePlaceParams } from '@/lib/paths'

/** A recommendation's revisions, /p/:place/r/:rec/history. Placeholder until T031. */
export function RevisionsPage() {
  const { t } = useTranslation('edit')
  const params = usePlaceParams()
  if (!params) {
    return <InvalidLinkPage />
  }
  return <ComingSoonPage title={t('history.title')} />
}
