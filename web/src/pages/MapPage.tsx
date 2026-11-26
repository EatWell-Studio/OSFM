import { useTranslation } from 'react-i18next'
import { PageHeading } from '@/components/PageHeading'

/** Placeholder. The map (T015). */
export function MapPage() {
  const { t } = useTranslation()
  return <PageHeading>{t('pages.map.title')}</PageHeading>
}
