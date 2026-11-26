import { useQuery, useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useEffect, useState } from 'react'
import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { checkInvite, inviteCodeFromHash, isWellFormedInviteCode } from '@/invites/invites'
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

/** The ways to register (D3); each is its own component, the page only offers the choice. */
const methods: {
  id: string
  label: (t: TFunction) => string
  // onRegistered gets the member when the method's response carries it; without, the session is fetched again.
  form: (props: { invite: string; onRegistered: (me?: Me) => void }) => ReactNode
}[] = [
  {
    id: 'password',
    label: (t) => t('pages.register.methods.password'),
    form: (props) => <PasswordTotpRegistration {...props} />,
  },
  {
    id: 'passkey',
    label: (t) => t('pages.register.methods.passkey'),
    form: ({ invite, onRegistered }) => (
      <PasskeyRegistration
        code={invite}
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
  const { t } = useTranslation()
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
    retry: 1,
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
    content = <p>{t('pages.register.noInvite')}</p>
  } else if (!wellFormed || check.data === false) {
    content = (
      <p role="alert" className="text-destructive">
        {t('pages.register.invalidInvite')}
      </p>
    )
  } else if (check.data === true) {
    content = (
      <>
        <p role="status">{t('pages.register.validInvite')}</p>
        {methods.length > 1 && (
          <div role="group" aria-label={t('pages.register.method')} className="flex flex-wrap gap-2">
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
        <p>{t('pages.register.checkError')}</p>
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
      <PageHeading>{t('pages.register.title')}</PageHeading>
      <div className="flex flex-col gap-3 px-4">{content}</div>
    </>
  )
}
