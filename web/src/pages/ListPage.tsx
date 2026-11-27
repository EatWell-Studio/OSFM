import { useTranslation } from 'react-i18next'
import { ComingSoonPage } from '@/components/ComingSoon'

/** The places in the current view, by distance, /list (docs/architecture.md §4.2). Placeholder until T028. */
export function ListPage() {
  const { t } = useTranslation('map')
  return <ComingSoonPage title={t('list.title')} />
}
