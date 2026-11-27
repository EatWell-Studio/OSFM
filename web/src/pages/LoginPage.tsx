import { useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { type ReactNode, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate, useSearchParams } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { PasskeySignIn } from '@/login/PasskeySignIn'
import { PasswordSignIn } from '@/login/PasswordSignIn'
import { RecoverySignIn } from '@/login/RecoverySignIn'
import { sanitizeNext } from '@/lib/next'
import { type Me, sessionQueryKey } from '@/session/session'
import { useSession } from '@/session/useSession'

/** The ways to sign in (D4); each is its own component, the page only offers the choice. */
const methods: {
  id: string
  label: (t: TFunction<'auth'>) => string
  // onSignedIn gets the member when the method's response carries it; without, the session is fetched again.
  form: (props: { onSignedIn: (me?: Me) => void }) => ReactNode
}[] = [
  {
    id: 'password',
    label: (t) => t('login.methods.password'),
    form: (props) => <PasswordSignIn {...props} />,
  },
  {
    id: 'passkey',
    label: (t) => t('login.methods.passkey'),
    form: ({ onSignedIn }) => (
      <PasskeySignIn
        onSignedIn={() => {
          onSignedIn()
        }}
      />
    ),
  },
]

/**
 * Sign-in. A signed-in member is sent on to `next`, the page they wanted before being asked to sign in; a successful
 * sign-in does the same.
 */
export function LoginPage() {
  const { t } = useTranslation('auth')
  const [searchParams] = useSearchParams()
  const session = useSession()
  const queryClient = useQueryClient()
  const [method, setMethod] = useState('password')
  // The recovery of a passkey-only member who lost their passkeys, instead of the sign-in methods.
  const [recovering, setRecovering] = useState(false)
  // After a recovery that left old passkeys, the member checks them on their page first.
  const [checkPasskeys, setCheckPasskeys] = useState(false)
  if (session.status === 'signed-in') {
    return checkPasskeys ? (
      <Navigate to="/me" replace state={{ recovered: true }} />
    ) : (
      <Navigate to={sanitizeNext(searchParams.get('next'))} replace />
    )
  }
  const signedIn = (me?: Me) => {
    // The page re-renders as signed in and navigates on.
    if (me) {
      queryClient.setQueryData(sessionQueryKey, me)
    } else {
      void queryClient.invalidateQueries({ queryKey: sessionQueryKey })
    }
  }
  if (recovering) {
    return (
      <>
        <PageHeading>{t('credentials.recover.title')}</PageHeading>
        <div className="flex flex-col gap-4 px-4 pb-4">
          <RecoverySignIn
            onDone={(enrolled) => {
              // The page re-renders as signed in and navigates on.
              setCheckPasskeys(enrolled.otherPasskeys > 0)
              queryClient.setQueryData(sessionQueryKey, enrolled.me)
            }}
          />
          <Button
            variant="link"
            className="self-start px-0"
            onClick={() => {
              setRecovering(false)
            }}
          >
            {t('credentials.recover.back')}
          </Button>
        </div>
      </>
    )
  }
  return (
    <>
      <PageHeading>{t('login.title')}</PageHeading>
      <div className="flex flex-col gap-4 px-4 pb-4">
        {methods.length > 1 && (
          <div role="group" aria-label={t('login.method')} className="flex flex-wrap gap-2">
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
        <Button
          variant="link"
          className="self-start px-0"
          onClick={() => {
            setRecovering(true)
          }}
        >
          {t('credentials.recover.entry')}
        </Button>
      </div>
    </>
  )
}
