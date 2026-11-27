import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** The invitation tree and branch actions, /admin/tree. Placeholder until T035. */
export function TreeAdmin() {
  const { t } = useTranslation('members')
  return (
    <div className="px-4">
      <ComingSoonSection title={t('tree.title')} />
    </div>
  )
}
