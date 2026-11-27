import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** The review queue of reported and hidden recommendations, /admin/reports. Placeholder until T036. */
export function ReportsAdmin() {
  const { t } = useTranslation('reports')
  return (
    <div className="px-4">
      <ComingSoonSection title={t('title')} />
    </div>
  )
}
