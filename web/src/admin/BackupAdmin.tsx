import { useTranslation } from 'react-i18next'
import { ComingSoonSection } from '@/components/ComingSoon'

/** Creating and downloading backups, /admin/backup. Placeholder until T042. */
export function BackupAdmin() {
  const { t } = useTranslation('backup')
  return (
    <div className="px-4">
      <ComingSoonSection title={t('title')} />
    </div>
  )
}
