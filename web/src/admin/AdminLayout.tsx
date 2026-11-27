import { useTranslation } from 'react-i18next'
import { NavLink, Outlet } from 'react-router'
import { PageHeading } from '@/components/PageHeading'
import { adminPath, adminSections } from '@/lib/paths'
import { cn } from '@/lib/utils'
import { useSession } from '@/session/useSession'

/**
 * The maintainers' administration, /admin/<section>: a row of links to the sections and the section's page below. Each
 * section is its own route and file (InvitesAdmin, MembersAdmin, …). Members who are not maintainers see that it is
 * not for them, on every section; the server refuses them the maintainer routes in any case.
 */
export function AdminLayout() {
  const { t } = useTranslation('admin')
  const session = useSession()
  const isMaintainer = session.status === 'signed-in' && session.me.role === 'maintainer'
  return (
    <>
      <PageHeading>{t('title')}</PageHeading>
      {isMaintainer ? (
        <>
          <nav aria-label={t('nav.label')} className="overflow-x-auto px-4 pb-4">
            <ul className="flex gap-1">
              {adminSections.map((section) => (
                <li key={section}>
                  <NavLink
                    to={adminPath(section)}
                    className={({ isActive }) =>
                      cn(
                        'inline-flex h-8 items-center rounded-lg px-3 text-sm whitespace-nowrap text-muted-foreground',
                        'outline-none focus-visible:ring-3 focus-visible:ring-ring/50',
                        isActive && 'bg-secondary font-medium text-foreground',
                      )
                    }
                  >
                    {t(`nav.${section}`)}
                  </NavLink>
                </li>
              ))}
            </ul>
          </nav>
          <div className="flex flex-col gap-8 pb-4">
            <Outlet />
          </div>
        </>
      ) : (
        <p role="alert" className="px-4 text-muted-foreground">
          {t('notAllowed')}
        </p>
      )}
    </>
  )
}
