import { Outlet } from 'react-router'
import { BottomNav } from './BottomNav'
import { TopBar } from './TopBar'

/** The layout of the signed-in app: top bar, page, bottom navigation (mobile first). */
export function AppShell() {
  return (
    <div className="flex min-h-dvh flex-col">
      <TopBar />
      {/* Bottom padding keeps the end of the page clear of the fixed navigation. */}
      <main className="flex-1 pb-[calc(3.5rem+env(safe-area-inset-bottom))]">
        <Outlet />
      </main>
      <BottomNav />
    </div>
  )
}
