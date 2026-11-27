import { LogOut, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { Button } from '@/components/ui/button'
import { useLogout } from '@/session/useLogout'
import { useSession } from '@/session/useSession'

/** Signing out, and for maintainers the way to the administration. */
export function AccountSection() {
  const { t } = useTranslation('me')
  const logout = useLogout()
  const session = useSession()
  return (
    <div className="flex flex-col items-start gap-3">
      {session.status === 'signed-in' && session.me.role === 'maintainer' && (
        <Button variant="outline" asChild>
          <Link to="/admin">
            <Settings aria-hidden="true" />
            {t('admin')}
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
        {t('logout')}
      </Button>
      {logout.isError && (
        <p role="alert" className="text-sm text-destructive">
          {t('logoutError')}
        </p>
      )}
    </div>
  )
}
