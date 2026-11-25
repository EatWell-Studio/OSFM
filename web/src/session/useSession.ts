import { useQuery } from '@tanstack/react-query'
import { type Me, sessionQuery } from './session'

export type Session =
  | { status: 'loading' }
  | { status: 'error'; retry: () => void }
  | { status: 'signed-out' }
  | { status: 'signed-in'; me: Me }

/** The session state, from GET /api/osfm/me. */
export function useSession(): Session {
  const query = useQuery(sessionQuery)
  // A failed background refetch keeps the last known answer.
  if (query.data !== undefined) {
    return query.data ? { status: 'signed-in', me: query.data } : { status: 'signed-out' }
  }
  if (query.isError) {
    return {
      status: 'error',
      retry: () => {
        void query.refetch()
      },
    }
  }
  return { status: 'loading' }
}
