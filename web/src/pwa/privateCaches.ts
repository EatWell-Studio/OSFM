// The private caches of the service worker (src/sw/rules.ts; docs/architecture.md §4.5) hold the map data and the
// tiles a member has seen. On a shared device the next person must not see them, so the app deletes them:
//
//   - when the member signs out (useLogout), and whenever the session turns out to have ended (a 401, the server
//     ending the session, its expiry: the session query answers "signed out"; src/lib/queryClient.ts);
//   - before a member uses the app on a device whose caches were filled for somebody else, or for an unknown member
//     (RequireSession, through useCacheOwner): the member the caches belong to is remembered on the device.

import { useQuery } from '@tanstack/react-query'
import { clearPrivateMessage, isPrivateCacheName } from '@/sw/rules'

/** The member (uuid) the private caches were filled for, in localStorage. */
const ownerKey = 'osfm.cacheOwner'

/** How long to wait for the service worker's answer before deleting the caches from the page alone. */
const answerTimeoutMillis = 3000

/** The member the private caches belong to, or null when none is known (or storage is not available). */
export function cacheOwner(): string | null {
  try {
    return localStorage.getItem(ownerKey)
  } catch {
    return null
  }
}

function setCacheOwner(uuid: string | null) {
  try {
    if (uuid === null) {
      localStorage.removeItem(ownerKey)
    } else {
      localStorage.setItem(ownerKey, uuid)
    }
  } catch {
    // Without storage no owner is ever known, and the caches are deleted every time a member starts the app.
  }
}

/** Asks the service worker that controls the page to delete the private caches, and waits for its answer. */
function askServiceWorker(worker: ServiceWorker): Promise<void> {
  return new Promise((done) => {
    const channel = new MessageChannel()
    const timer = setTimeout(done, answerTimeoutMillis)
    channel.port1.onmessage = () => {
      clearTimeout(timer)
      done()
    }
    worker.postMessage({ type: clearPrivateMessage }, [channel.port2])
  })
}

/**
 * Deletes every private cache. The service worker does it first (so that a response still on its way is not cached
 * afterwards); the page then deletes whatever is left, which also covers a page without a service worker.
 */
export async function clearPrivateCaches(): Promise<void> {
  // navigator.serviceWorker, too, exists only in secure contexts.
  const worker =
    typeof navigator !== 'undefined' && 'serviceWorker' in navigator ? navigator.serviceWorker.controller : null
  if (worker) {
    await askServiceWorker(worker)
  }
  // Cache Storage exists only in secure contexts (https, localhost).
  if (typeof caches === 'undefined') {
    return
  }
  const names = await caches.keys()
  await Promise.all(names.filter(isPrivateCacheName).map((name) => caches.delete(name)))
}

/** Deletes the private caches and forgets whom they belonged to: on sign-out and when the session has ended. */
export async function forgetPrivateData(): Promise<void> {
  setCacheOwner(null)
  await clearPrivateCaches()
}

/**
 * Makes the private caches the member's: when they belong to somebody else, or to nobody known, they are deleted
 * first. Resolves once the caches may be used.
 */
export async function adoptPrivateCaches(uuid: string): Promise<void> {
  if (cacheOwner() === uuid) {
    return
  }
  await clearPrivateCaches()
  setCacheOwner(uuid)
}

/**
 * Whether the private caches may be used by the member: at once when they are the member's already, otherwise once
 * adoptPrivateCaches has deleted the previous contents. Pages that fetch member data wait for it.
 */
export function useCacheOwner(uuid: string | null): boolean {
  const owned = uuid !== null && cacheOwner() === uuid
  const adopted = useQuery({
    queryKey: ['cacheOwner', uuid],
    queryFn: async () => {
      if (uuid !== null) {
        await adoptPrivateCaches(uuid)
      }
      return true
    },
    enabled: uuid !== null && !owned,
    staleTime: Infinity,
    retry: false,
  })
  // A failure to delete (storage broken in some way) cannot be helped by waiting: the app goes on.
  return owned || adopted.isSuccess || adopted.isError
}
