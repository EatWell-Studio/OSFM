import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** The member's notices at the top of the "me" page. Placeholder until T037. */
export function NoticesSection() {
  const { t } = useTranslation('notices')
  return <ComingSoonSection title={t('title')} />
}
