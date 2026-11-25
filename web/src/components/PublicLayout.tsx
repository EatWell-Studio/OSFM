import { Outlet } from 'react-router'
import { TopBar } from './TopBar'

/** The layout of the pages reachable without a session (sign-in, registration). */
export function PublicLayout() {
  return (
    <div className="flex min-h-dvh flex-col">
      <TopBar />
      <main className="flex-1">
        <Outlet />
      </main>
    </div>
  )
}
