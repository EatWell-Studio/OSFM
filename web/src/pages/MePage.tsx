import { LogOut } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { PageHeading } from '@/components/PageHeading'
import { Button } from '@/components/ui/button'
import { useLogout } from '@/session/useLogout'

/** Placeholder for the profile, pseudonym, credentials and data export; for now only signing out. */
export function MePage() {
  const { t } = useTranslation()
  const logout = useLogout()
  return (
    <>
      <PageHeading>{t('pages.me.title')}</PageHeading>
      <div className="flex flex-col items-start gap-3 p-4">
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
