import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** The download of all of the member's own data on the "me" page. Placeholder until T040. */
export function PersonalExport() {
  const { t } = useTranslation('personalExport')
  return <ComingSoonSection title={t('title')} />
}
