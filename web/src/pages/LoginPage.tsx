import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Navigate, useSearchParams } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { sanitizeNext } from '@/lib/next'
import { PasskeySignIn } from '@/login/PasskeySignIn'
import { sessionQueryKey } from '@/session/session'
import { useSession } from '@/session/useSession'

/**
 * Sign-in: the methods are components of their own (login/). A signed-in member is sent on to `next`, the page they
 * wanted before being asked to sign in; after a successful sign-in the session is fetched again, which does the same.
 */
export function LoginPage() {
  const { t } = useTranslation()
  const [searchParams] = useSearchParams()
  const session = useSession()
  const queryClient = useQueryClient()
  if (session.status === 'signed-in') {
    return <Navigate to={sanitizeNext(searchParams.get('next'))} replace />
  }
  return (
    <>
      <PageHeading>{t('pages.login.title')}</PageHeading>
      <div className="flex flex-col gap-3 px-4">
        <PasskeySignIn
          onSignedIn={() => {
            void queryClient.invalidateQueries({ queryKey: sessionQueryKey })
          }}
        />
      </div>
    </>
  )
}
