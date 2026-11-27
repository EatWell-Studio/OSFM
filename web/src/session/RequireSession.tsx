import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { loginPathFor, setupPagePath } from '@/lib/next'
import { setupQuery } from '@/setup/setup'
import { SessionPending } from './SessionGate'
import { useSession } from './useSession'

/**
 * Renders children for a signed-in member; otherwise redirects to the sign-in page, which returns here afterwards (or
 * leads to the setup wizard while the instance has no maintainer). A maintainer who has not yet given the instance its
 * cities is sent back to the wizard, which resumes at that step.
 */
export function RequireSession({ children }: { children: ReactNode }) {
  const session = useSession()
  const location = useLocation()
  const maintainer = session.status === 'signed-in' && session.me.role === 'maintainer'
  const setup = useQuery({ ...setupQuery(maintainer ? session.me.uuid : null), enabled: maintainer })
  if (session.status === 'signed-out') {
    return <Navigate to={loginPathFor(location)} replace />
  }
  if (session.status !== 'signed-in') {
    return <SessionPending session={session} />
  }
  if (maintainer && setup.data === 'cities') {
    return <Navigate to={setupPagePath} replace />
  }
  if (maintainer && setup.isPending) {
    return <SessionPending session={{ status: 'loading' }} />
  }
  return children
}
