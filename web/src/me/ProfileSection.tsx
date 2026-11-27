import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** The member's main name, pseudonym and external links on the "me" page. Placeholder until T033. */
export function ProfileSection() {
  const { t } = useTranslation('profile')
  return <ComingSoonSection title={t('title')} />
}
