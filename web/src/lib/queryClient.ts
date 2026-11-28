import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { forgetPrivateData } from '@/pwa/privateCaches'
import type { Me } from '@/session/session'
import { sessionQueryKey } from '@/session/session'
import { ApiError } from './api'

function isSessionKey(key: readonly unknown[]): boolean {
  return key.length === sessionQueryKey.length && key[0] === sessionQueryKey[0]
}

/**
 * A query client whose requests, when answered with 401, make the app re-check the session (and so sign-in). When the
 * session turns out to have ended, or to be another member's, everything fetched for the previous member is dropped:
 * the queries in memory and the service worker's private caches (src/pwa/privateCaches.ts), so that the next person on
 * a shared device sees none of it.
 */
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
  // The member of the last known session: undefined before the first answer.
  let member: string | null | undefined
  client.getQueryCache().subscribe((event) => {
    if (
      event.type !== 'updated' ||
      event.action.type !== 'success' ||
      !isSessionKey(event.query.queryKey as unknown[])
    ) {
      return
    }
    const me = event.query.state.data as Me | null | undefined
    const now = me ? me.uuid : null
    if (member !== undefined && member !== null && member !== now) {
      client.removeQueries({ predicate: (query) => !isSessionKey(query.queryKey) })
    }
    if (now === null) {
      void forgetPrivateData().catch(() => undefined)
    }
    member = now
  })
  return client
}
