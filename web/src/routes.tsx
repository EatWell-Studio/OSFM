import { Navigate, type RouteObject } from 'react-router'
import { AppShell } from '@/components/AppShell'
import { PublicLayout } from '@/components/PublicLayout'
import { loginPath, registerPath, resetPath, setupPagePath } from '@/lib/next'
import { AdminLayout } from '@/admin/AdminLayout'
import { BackupAdmin } from '@/admin/BackupAdmin'
import { InvitesAdmin } from '@/admin/InvitesAdmin'
import { MembersAdmin } from '@/admin/MembersAdmin'
import { ReportsAdmin } from '@/admin/ReportsAdmin'
import { SettingsAdmin } from '@/admin/SettingsAdmin'
import { TreeAdmin } from '@/admin/TreeAdmin'
import { NewPlacePage } from '@/create/NewPlacePage'
import { RecommendPage } from '@/create/RecommendPage'
import { EditPlacePage } from '@/edit/EditPlacePage'
import { EditRecommendationPage } from '@/edit/EditRecommendationPage'
import { RevisionsPage } from '@/edit/RevisionsPage'
import { routePatterns } from '@/lib/paths'
import { PlacePage } from '@/place/PlacePage'
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
//
// Every path of M2 is registered here already, each with its own page file (placeholders until the task that builds
// it), so that the features add no routes of their own: the paths are built with src/lib/paths.ts. No last segment
// contains a dot and nothing is under /assets or /map, which the server answers with 404 (docs/architecture.md §4.5).
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
      { path: routePatterns.list, element: <ListPage /> },
      { path: routePatterns.newPlace, element: <NewPlacePage /> },
      { path: routePatterns.place, element: <PlacePage /> },
      { path: routePatterns.editPlace, element: <EditPlacePage /> },
      { path: routePatterns.recommend, element: <RecommendPage /> },
      { path: routePatterns.editRecommendation, element: <EditRecommendationPage /> },
      { path: routePatterns.revisions, element: <RevisionsPage /> },
      { path: 'me', element: <MePage /> },
      {
        path: 'admin',
        element: <AdminLayout />,
        children: [
          { index: true, element: <Navigate to="invites" replace /> },
          { path: 'invites', element: <InvitesAdmin /> },
          { path: 'members', element: <MembersAdmin /> },
          { path: 'tree', element: <TreeAdmin /> },
          { path: 'reports', element: <ReportsAdmin /> },
          { path: 'settings', element: <SettingsAdmin /> },
          { path: 'backup', element: <BackupAdmin /> },
        ],
      },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]
