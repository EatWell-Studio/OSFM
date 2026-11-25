import { useTranslation } from 'react-i18next'
import { PageHeading } from '@/components/PageHeading'

/** Placeholder. Profile, pseudonym, credentials and data export. */
export function MePage() {
  const { t } = useTranslation()
  return <PageHeading>{t('pages.me.title')}</PageHeading>
}
