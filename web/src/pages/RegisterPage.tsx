import { useQuery, useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useEffect, useState } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { checkInvite, inviteCodeFromHash, isWellFormedInviteCode } from '@/invites/invites'
import { ApiError } from '@/lib/api'
import { rateLimitMessage } from '@/lib/rateLimit'
import { PasskeyRegistration } from '@/register/PasskeyRegistration'
import { PasswordTotpRegistration } from '@/register/PasswordTotpRegistration'
import { type Me, sessionQueryKey } from '@/session/session'

// The code from the invite link is kept for this tab only, so that reloading the page still finds it after the
// fragment has been removed from the address bar.
const storageKey = 'osfm.inviteCode'

function storedCode(): string | null {
  try {
    return sessionStorage.getItem(storageKey)
  } catch {
    return null
  }
}

function storeCode(code: string) {
  try {
    sessionStorage.setItem(storageKey, code)
  } catch {
    // Without storage a reload just needs the link again.
  }
}

function forgetCode() {
  try {
    sessionStorage.removeItem(storageKey)
  } catch {
    // Nothing stored.
  }
}

function isRateLimited(error: unknown): error is ApiError {
  return error instanceof ApiError && error.status === 429
}

/** The ways to register (D3); each is its own component, the page only offers the choice. */
const methods: {
  id: string
  label: (t: TFunction<'auth'>) => string
  // onRegistered gets the member when the method's response carries it; without, the session is fetched again.
  form: (props: { invite: string; onRegistered: (me?: Me) => void }) => ReactNode
}[] = [
  {
    id: 'password',
    label: (t) => t('register.methods.password'),
    form: ({ invite, onRegistered }) => (
      <PasswordTotpRegistration entitlement={{ invite }} onRegistered={onRegistered} />
    ),
  },
  {
    id: 'passkey',
    label: (t) => t('register.methods.passkey'),
    form: ({ invite, onRegistered }) => (
      <PasskeyRegistration
        entitlement={{ invite }}
        onDone={() => {
          onRegistered()
        }}
      />
    ),
  },
]

/**
 * Registration with an invite (D1). Reads the code from the invite link's fragment (/register#invite=…), removes it
 * from the address bar and asks the server whether it can be used; then offers the registration methods (D3). The
 * server checks the invite again when the member is created. Once the member has saved the recovery codes, the app
 * opens the map.
 */
export function RegisterPage() {
  const { t } = useTranslation('auth')
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [method, setMethod] = useState('password')
  const [code] = useState(() => {
    const fromLink = inviteCodeFromHash(location.hash)
    if (fromLink !== null) {
      storeCode(fromLink)
      return fromLink
    }
    return storedCode()
  })
  const wellFormed = code !== null && isWellFormedInviteCode(code)

  // Out of the address bar and the history entry, now that it has been read.
  useEffect(() => {
    if (inviteCodeFromHash(location.hash) !== null) {
      void navigate({ pathname: location.pathname, search: location.search }, { replace: true })
    }
  }, [location.hash, location.pathname, location.search, navigate])

  const check = useQuery({
    queryKey: ['inviteCheck', code],
    queryFn: () => checkInvite(code ?? ''),
    enabled: wellFormed,
    staleTime: Infinity,
    // Retrying too many attempts only makes the wait longer.
    retry: (failures, error) => failures < 1 && !isRateLimited(error),
  })

  const registered = (me?: Me) => {
    forgetCode()
    if (me) {
      queryClient.setQueryData(sessionQueryKey, me)
    } else {
      void queryClient.invalidateQueries({ queryKey: sessionQueryKey })
    }
    void navigate('/', { replace: true })
  }

  let content
  if (code === null) {
    content = <p>{t('register.noInvite')}</p>
  } else if (!wellFormed || check.data === false) {
    content = (
      <p role="alert" className="text-destructive">
        {t('register.invalidInvite')}
      </p>
    )
  } else if (check.data === true) {
    content = (
      <>
        <p role="status">{t('register.validInvite')}</p>
        {methods.length > 1 && (
          <div role="group" aria-label={t('register.method')} className="flex flex-wrap gap-2">
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
        {methods.find((m) => m.id === method)?.form({ invite: code, onRegistered: registered })}
      </>
    )
  } else if (check.isError) {
    content = (
      <div role="alert" className="flex flex-col items-start gap-3">
        <p>{isRateLimited(check.error) ? rateLimitMessage(check.error.retryAfter) : t('register.checkError')}</p>
        <Button
          onClick={() => {
            void check.refetch()
          }}
        >
          {t('register.retry')}
        </Button>
      </div>
    )
  } else {
    content = (
      <p role="status" className="text-muted-foreground">
        {t('register.checking')}
      </p>
    )
  }

  return (
    <>
      <PageHeading>{t('register.title')}</PageHeading>
      <div className="flex flex-col gap-3 px-4">{content}</div>
    </>
  )
}
