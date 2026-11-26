import { useTranslation } from 'react-i18next'
import { PageHeading } from '@/components/PageHeading'

/** Placeholder. Maintainer administration. Only checks for a session so far; the maintainer role check comes with the member and invitation work. */
export function AdminPage() {
  const { t } = useTranslation()
  return <PageHeading>{t('pages.admin.title')}</PageHeading>
}
