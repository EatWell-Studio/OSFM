import { useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate, useSearchParams } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { PasswordSignIn } from '@/login/PasswordSignIn'
import { sanitizeNext } from '@/lib/next'
import { type Me, sessionQueryKey } from '@/session/session'
import { useSession } from '@/session/useSession'

/** The ways to sign in (D4); each is its own component, the page only offers the choice. */
const methods: {
  id: string
  label: (t: TFunction) => string
  form: (props: { onSignedIn: (me: Me) => void }) => ReactNode
}[] = [
  {
    id: 'password',
    label: (t) => t('pages.login.methods.password'),
    form: (props) => <PasswordSignIn {...props} />,
  },
]

/**
 * Sign-in. A signed-in member is sent on to `next`, the page they wanted before being asked to sign in; a successful
 * sign-in does the same.
 */
export function LoginPage() {
  const { t } = useTranslation()
  const [searchParams] = useSearchParams()
  const session = useSession()
  const queryClient = useQueryClient()
  const [method, setMethod] = useState('password')
  if (session.status === 'signed-in') {
    return <Navigate to={sanitizeNext(searchParams.get('next'))} replace />
  }
  const signedIn = (me: Me) => {
    // The page re-renders as signed in and navigates on.
    queryClient.setQueryData(sessionQueryKey, me)
  }
  return (
    <>
      <PageHeading>{t('pages.login.title')}</PageHeading>
      <div className="flex flex-col gap-4 px-4 pb-4">
        {methods.length > 1 && (
          <div role="group" aria-label={t('pages.login.method')} className="flex flex-wrap gap-2">
            {methods.map((m) => (
              <Button
                key={m.id}
                variant={m.id === method ? 'default' : 'outline'}
                aria-pressed={m.id === method}
                onClick={() => {
                  setMethod(m.id)
                }}
              >
                {m.label(t)}
              </Button>
            ))}
          </div>
        )}
        {methods.find((m) => m.id === method)?.form({ onSignedIn: signedIn })}
      </div>
    </>
  )
}
