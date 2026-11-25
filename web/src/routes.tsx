import type { RouteObject } from 'react-router'
import { AppShell } from '@/components/AppShell'
import { PublicLayout } from '@/components/PublicLayout'
import { loginPath, registerPath } from '@/lib/next'
import { AdminPage } from '@/pages/AdminPage'
import { ListPage } from '@/pages/ListPage'
import { LoginPage } from '@/pages/LoginPage'
import { MapPage } from '@/pages/MapPage'
import { MePage } from '@/pages/MePage'
import { NotFoundPage } from '@/pages/NotFoundPage'
import { RegisterPage } from '@/pages/RegisterPage'
import { RequireSession } from '@/session/RequireSession'

// Without a session only sign-in and registration are reachable (E1); every other path, unknown ones included,
// redirects to sign-in first.
export const routes: RouteObject[] = [
  {
    element: <PublicLayout />,
    children: [
      { path: loginPath, element: <LoginPage /> },
      { path: registerPath, element: <RegisterPage /> },
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
