import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation, useNavigate } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { checkInvite, inviteCodeFromHash, isWellFormedInviteCode } from '@/invites/invites'

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

/**
 * Registration with an invite (D1). Reads the code from the invite link's fragment (/register#invite=…), removes it
 * from the address bar and asks the server whether it can be used. The registration forms themselves (passkey, or
 * password and TOTP) come with T009 and T010; the server checks the invite again when the member is created.
 */
export function RegisterPage() {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
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
        <p className="text-muted-foreground">{t('pages.register.formsPending')}</p>
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
