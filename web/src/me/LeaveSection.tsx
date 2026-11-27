import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** Leaving the circle, at the end of the "me" page. Placeholder until T038. */
export function LeaveSection() {
  const { t } = useTranslation('leave')
  return <ComingSoonSection title={t('title')} />
}
