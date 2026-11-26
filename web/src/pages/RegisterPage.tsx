import { useTranslation } from 'react-i18next'
import { PageHeading } from '@/components/PageHeading'

/** Placeholder. Registration with an invitation (T010). */
export function RegisterPage() {
  const { t } = useTranslation()
  return <PageHeading>{t('pages.register.title')}</PageHeading>
}
