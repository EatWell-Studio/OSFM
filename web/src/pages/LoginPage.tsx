import { useTranslation } from 'react-i18next'
import { Navigate, useSearchParams } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { sanitizeNext } from '@/lib/next'
import { useSession } from '@/session/useSession'

/**
 * Placeholder for the sign-in form (T009). A signed-in member is sent on to `next`, the page they wanted before being
 * asked to sign in; the form will do the same after a successful sign-in.
 */
export function LoginPage() {
  const { t } = useTranslation()
  const [searchParams] = useSearchParams()
  const session = useSession()
  if (session.status === 'signed-in') {
    return <Navigate to={sanitizeNext(searchParams.get('next'))} replace />
  }
  return <PageHeading>{t('pages.login.title')}</PageHeading>
}
