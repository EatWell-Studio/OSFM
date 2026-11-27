import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** The member's own invites on the "me" page, with the cooling-off period. Placeholder until T034. */
export function InvitesSection() {
  const { t } = useTranslation('invites')
  return <ComingSoonSection title={t('title')} />
}
