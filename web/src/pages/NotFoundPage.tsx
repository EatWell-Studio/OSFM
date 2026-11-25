import { useTranslation } from 'react-i18next'
import { PageHeading } from '@/components/PageHeading'

/** Placeholder. Any path that is not a route. */
export function NotFoundPage() {
  const { t } = useTranslation()
  return <PageHeading>{t('pages.notFound.title')}</PageHeading>
}
