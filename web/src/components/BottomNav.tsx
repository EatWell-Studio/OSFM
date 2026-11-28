import { useQuery } from '@tanstack/react-query'
import type { LucideIcon } from 'lucide-react'
import { ListIcon, MapIcon, UserIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router'
import { useMapSearch } from '@/filters/useFilters'
import { noticesQuery } from '@/me/noticesApi'
import { cn } from '@/lib/utils'

interface Item {
  to: string
  labelKey: 'nav.map' | 'nav.list' | 'nav.me'
  Icon: LucideIcon
  /** Whether the link keeps the map's city and filters (A6). */
  keepsMapSearch?: boolean
}

const items: Item[] = [
  { to: '/', labelKey: 'nav.map', Icon: MapIcon, keepsMapSearch: true },
  { to: '/list', labelKey: 'nav.list', Icon: ListIcon, keepsMapSearch: true },
  { to: '/me', labelKey: 'nav.me', Icon: UserIcon },
]

/**
 * The bottom navigation of the signed-in app. Each entry shows its label; the icons are decorative. "Me" carries the
 * number of unread notices (M2-Q14), read out with the label.
 */
export function BottomNav() {
  const { t } = useTranslation(['common', 'notices'])
  const mapSearch = useMapSearch()
  const notices = useQuery(noticesQuery)
  const unread = notices.data?.unread ?? 0
  return (
    <nav
      aria-label={t('nav.label')}
      className="fixed inset-x-0 bottom-0 z-10 border-t bg-background pb-[env(safe-area-inset-bottom)]"
    >
      <ul className="mx-auto flex max-w-lg">
        {items.map(({ to, labelKey, Icon, keepsMapSearch }) => (
          <li key={to} className="flex-1">
            <NavLink
              to={keepsMapSearch ? { pathname: to, search: mapSearch } : to}
              end
              className={({ isActive }) =>
                cn(
                  'flex h-14 flex-col items-center justify-center gap-0.5 text-xs text-muted-foreground',
                  'outline-none focus-visible:ring-3 focus-visible:ring-ring/50',
                  isActive && 'font-medium text-foreground',
                )
              }
            >
              <span className="relative">
                <Icon aria-hidden="true" className="size-5" />
                {to === '/me' && unread > 0 && <UnreadBadge count={unread} />}
              </span>
              <span>{t(labelKey)}</span>
              {to === '/me' && unread > 0 && (
                <span className="sr-only">{t('notices:unreadBadge', { count: unread })}</span>
              )}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}

/** The unread count on "me": up to 99, then 99+. Screen readers hear the count after the label instead. */
function UnreadBadge({ count }: { count: number }) {
  return (
    <span
      aria-hidden="true"
      data-testid="unread-notices"
      className={cn(
        'absolute -top-1.5 left-3 min-w-4 rounded-full bg-destructive px-1 text-center text-[0.625rem] leading-4',
        'font-medium text-white',
      )}
    >
      {count > 99 ? '99+' : count}
    </span>
  )
}
