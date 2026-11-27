import type { RouteObject } from 'react-router'
import { AppShell } from '@/components/AppShell'
import { PublicLayout } from '@/components/PublicLayout'
import { loginPath, registerPath, resetPath, setupPagePath } from '@/lib/next'
import { AdminPage } from '@/pages/AdminPage'
import { ListPage } from '@/pages/ListPage'
import { LoginPage } from '@/pages/LoginPage'
import { MapPage } from '@/pages/MapPage'
import { MePage } from '@/pages/MePage'
import { NotFoundPage } from '@/pages/NotFoundPage'
import { RegisterPage } from '@/pages/RegisterPage'
import { ResetPage } from '@/pages/ResetPage'
import { RequireSession } from '@/session/RequireSession'
import { SetupPage } from '@/setup/SetupPage'

// Without a session only sign-in, registration and the reset link are reachable (E1); every other path, unknown ones included,
// redirects to sign-in first. While the instance has no maintainer, the sign-in and registration pages lead to the
// setup wizard, which leads to the map once there is nothing left to set up (G3).
export const routes: RouteObject[] = [
  { path: setupPagePath, element: <SetupPage /> },
  {
    element: <PublicLayout />,
    children: [
      { path: loginPath, element: <LoginPage /> },
      { path: registerPath, element: <RegisterPage /> },
      { path: resetPath, element: <ResetPage /> },
    ],
  },
  {
    element: (
      <RequireSession>
        <AppShell />
      </RequireSession>
    ),
    children: [
      { index: true, element: <MapPage /> },
      { path: 'list', element: <ListPage /> },
      { path: 'me', element: <MePage /> },
      { path: 'admin', element: <AdminPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]
