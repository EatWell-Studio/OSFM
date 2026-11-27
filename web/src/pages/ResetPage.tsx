import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import {
  checkResetToken,
  type Enrolled,
  errorCode,
  parseEnrolled,
  resetEnrolTarget,
  resetTokenFromHash,
} from '@/credentials/api'
import { EnrolCredential } from '@/credentials/EnrolCredential'
import { credentialErrorMessage } from '@/credentials/messages'
import { RecoveryCodes } from '@/register/RecoveryCodes'
import { sessionQueryKey } from '@/session/session'

// The token from the reset link is kept for this tab only, so that reloading the page still finds it after the
// fragment has been removed from the address bar.
const storageKey = 'osfm.resetToken'

function stored(): string | null {
  try {
    return sessionStorage.getItem(storageKey)
  } catch {
    return null
  }
}

function store(token: string | null) {
  try {
    if (token === null) {
      sessionStorage.removeItem(storageKey)
    } else {
      sessionStorage.setItem(storageKey, token)
    }
  } catch {
    // Without storage a reload just needs the link again.
  }
}

/**
 * The reset link a maintainer created (/reset#token=…, D5): shows whose account it resets, lets the member set up a
 * new credential (password and authenticator app first, or a passkey), shows the new recovery codes and opens the
 * map. Every old credential and session is gone by then. An unusable link gets one message, whatever happened to it.
 */
export function ResetPage() {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [token] = useState(() => {
    const fromLink = resetTokenFromHash(location.hash)
    if (fromLink !== null) {
      store(fromLink)
      return fromLink
    }
    return stored()
  })
  const [enrolled, setEnrolled] = useState<Enrolled | null>(null)
  const [failure, setFailure] = useState<string | null>(null)

  useEffect(() => {
    if (resetTokenFromHash(location.hash) !== null) {
      void navigate({ pathname: location.pathname, search: location.search }, { replace: true })
    }
  }, [location.hash, location.pathname, location.search, navigate])

  const check = useQuery({
    queryKey: ['resetCheck', token],
    queryFn: () => checkResetToken(token ?? ''),
    enabled: token !== null,
    staleTime: Infinity,
    retry: false,
  })

  let content
  if (enrolled) {
    content = (
      <RecoveryCodes
        codes={enrolled.recoveryCodes}
        onConfirmed={() => {
          store(null)
          queryClient.setQueryData(sessionQueryKey, enrolled.me)
          void navigate('/', { replace: true })
        }}
      />
    )
  } else if (token === null || check.data?.valid === false || failure !== null) {
    content = (
      <p role="alert" className="text-destructive">
        {failure ?? t('credentials.reset.invalid')}
      </p>
    )
  } else if (check.data?.valid) {
    const { loginName, displayName } = check.data
    content = (
      <>
        <p role="status">{t('credentials.reset.account', { loginName, displayName })}</p>
        <p>{t('credentials.reset.explanation')}</p>
        <EnrolCredential
          target={resetEnrolTarget(token)}
          loginName={loginName}
          onEnrolled={(body) => {
            setEnrolled(parseEnrolled(body))
          }}
          onError={(err) => {
            if (errorCode(err) !== 'reset_invalid') {
              return false
            }
            store(null)
            setFailure(t('credentials.reset.invalid'))
            return true
          }}
        />
      </>
    )
  } else if (check.isError) {
    content = (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p>{credentialErrorMessage(t, check.error)}</p>
        <Button
          onClick={() => {
            void check.refetch()
          }}
        >
          {t('pages.register.retry')}
        </Button>
      </div>
    )
  } else {
    content = (
      <p role="status" className="text-muted-foreground">
        {t('pages.register.checking')}
      </p>
    )
  }

  return (
    <>
      <PageHeading>{t('credentials.reset.title')}</PageHeading>
      <div className="flex flex-col gap-3 px-4 pb-4">{content}</div>
    </>
  )
}
