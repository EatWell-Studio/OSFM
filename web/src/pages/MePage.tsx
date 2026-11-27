import { LogOut, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link, useLocation } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { CredentialsSection } from '@/credentials/CredentialsSection'
import { useLogout } from '@/session/useLogout'
import { useSession } from '@/session/useSession'

/**
 * The member's own page: the credentials (D5), signing out, and for maintainers the way to the administration page.
 * The profile, pseudonym and data export come later. After a recovery, the page says to check the old passkeys.
 */
export function MePage() {
  const { t } = useTranslation()
  const logout = useLogout()
  const session = useSession()
  const location = useLocation()
  const recovered = (location.state as { recovered?: boolean } | null)?.recovered === true
  return (
    <>
      <PageHeading>{t('pages.me.title')}</PageHeading>
      <div className="flex flex-col gap-4 px-4">
        {recovered && (
          <p role="status" className="rounded-lg border border-primary/40 p-3 text-sm">
            {t('credentials.recover.checkPasskeys')}
          </p>
        )}
        <CredentialsSection />
      </div>
      <div className="flex flex-col items-start gap-3 p-4">
        {session.status === 'signed-in' && session.me.role === 'maintainer' && (
          <Button variant="outline" asChild>
            <Link to="/admin">
              <Settings aria-hidden="true" />
              {t('pages.me.admin')}
            </Link>
          </Button>
        )}
        <Button
          variant="outline"
          disabled={logout.isPending}
          onClick={() => {
            logout.mutate()
          }}
        >
          <LogOut aria-hidden="true" />
          {t('pages.me.logout')}
        </Button>
        {logout.isError && (
          <p role="alert" className="text-sm text-destructive">
            {t('pages.me.logoutError')}
          </p>
        )}
      </div>
    </>
  )
}
