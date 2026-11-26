import { useTranslation } from 'react-i18next'
import { PageHeading } from '@/components/PageHeading'

/** Placeholder. The places in the current view, by distance (docs/architecture.md §4.2). */
export function ListPage() {
  const { t } = useTranslation()
  return <PageHeading>{t('pages.list.title')}</PageHeading>
}
