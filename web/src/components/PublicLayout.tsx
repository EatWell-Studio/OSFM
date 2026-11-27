import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Navigate, Outlet } from 'react-router'
import { setupPagePath } from '@/lib/next'
import { setupQuery } from '@/setup/setup'
import { TopBar } from './TopBar'

/**
 * The layout of the pages reachable without a session (sign-in, registration). While the instance has no maintainer
 * yet, they lead to the setup wizard instead (G3).
 */
export function PublicLayout() {
  const { t } = useTranslation()
  // Whether the instance has a maintainer is the same for everyone: the anonymous answer does.
  const setup = useQuery(setupQuery(null))
  if (setup.data === 'maintainer') {
    return <Navigate to={setupPagePath} replace />
  }
  return (
    <div className="flex min-h-dvh flex-col">
      <TopBar />
      <main className="flex-1">
        {setup.isPending ? (
          <p role="status" className="p-4 text-muted-foreground">
            {t('app.loading')}
          </p>
        ) : (
          <Outlet />
        )}
      </main>
    </div>
  )
}
