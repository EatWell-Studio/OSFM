import { LogOut, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { useLogout } from '@/session/useLogout'
import { useSession } from '@/session/useSession'

/**
 * Placeholder for the profile, pseudonym, credentials and data export; for now signing out, and for maintainers the way
 * to the administration page.
 */
export function MePage() {
  const { t } = useTranslation()
  const logout = useLogout()
  const session = useSession()
  return (
    <>
      <PageHeading>{t('pages.me.title')}</PageHeading>
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
