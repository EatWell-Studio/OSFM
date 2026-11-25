import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { sessionQueryKey } from '@/session/session'
import { ApiError } from './api'

/** A query client whose requests, when answered with 401, make the app re-check the session (and so sign-in). */
export function createQueryClient(): QueryClient {
  const onError = (error: Error) => {
    if (error instanceof ApiError && error.status === 401) {
      void client.invalidateQueries({ queryKey: sessionQueryKey })
    }
  }
  const client: QueryClient = new QueryClient({
    queryCache: new QueryCache({ onError }),
    mutationCache: new MutationCache({ onError }),
  })
  return client
}
