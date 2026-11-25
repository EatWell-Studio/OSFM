import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router'
import { loginPathFor } from '@/lib/next'
import { SessionPending } from './SessionGate'
import { useSession } from './useSession'

/** Renders children for a signed-in member; otherwise redirects to the sign-in page, which returns here afterwards. */
export function RequireSession({ children }: { children: ReactNode }) {
  const session = useSession()
  const location = useLocation()
  if (session.status === 'signed-out') {
    return <Navigate to={loginPathFor(location)} replace />
  }
  if (session.status !== 'signed-in') {
    return <SessionPending session={session} />
  }
  return children
}
