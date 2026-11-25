import type { LucideIcon } from 'lucide-react'
import { ListIcon, MapIcon, UserIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { NavLink } from 'react-router'
import { cn } from '@/lib/utils'

interface Item {
  to: string
  labelKey: 'nav.map' | 'nav.list' | 'nav.me'
  Icon: LucideIcon
}

const items: Item[] = [
  { to: '/', labelKey: 'nav.map', Icon: MapIcon },
  { to: '/list', labelKey: 'nav.list', Icon: ListIcon },
  { to: '/me', labelKey: 'nav.me', Icon: UserIcon },
]

/** The bottom navigation of the signed-in app. Each entry shows its label; the icons are decorative. */
export function BottomNav() {
  const { t } = useTranslation()
  return (
    <nav
      aria-label={t('nav.label')}
      className="fixed inset-x-0 bottom-0 z-10 border-t bg-background pb-[env(safe-area-inset-bottom)]"
    >
      <ul className="mx-auto flex max-w-lg">
        {items.map(({ to, labelKey, Icon }) => (
          <li key={to} className="flex-1">
            <NavLink
              to={to}
              end
              className={({ isActive }) =>
                cn(
                  'flex h-14 flex-col items-center justify-center gap-0.5 text-xs text-muted-foreground',
                  'outline-none focus-visible:ring-3 focus-visible:ring-ring/50',
                  isActive && 'font-medium text-foreground',
                )
              }
            >
              <Icon aria-hidden="true" className="size-5" />
              <span>{t(labelKey)}</span>
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  )
}
