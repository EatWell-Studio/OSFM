import { useQueryClient } from '@tanstack/react-query'
import { type ReactNode, useState } from 'react'
import { type Credentials, credentialsQueryKey, errorCode, isFresh, reauthRequiredCode } from './api'
import { VerifyPanel } from './VerifyPanel'

/**
 * Re-verification before sensitive changes (docs/architecture.md §3.3): guard(action) runs the action when the session
 * was verified recently enough, and otherwise first shows the verification panel, then runs it. handleError takes over
 * a request the server refused with reauth_required (the window ran out meanwhile): it shows the panel and returns
 * true, and the member repeats the step afterwards. panel is where the page renders the panel.
 */
export function useStepUp(credentials: Credentials | undefined): {
  guard: (action: () => void) => void
  handleError: (error: unknown) => boolean
  panel: ReactNode
} {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState<{ action: (() => void) | null } | null>(null)
  const guard = (action: () => void) => {
    if (isFresh(credentials)) {
      action()
    } else {
      setPending({ action })
    }
  }
  const handleError = (error: unknown) => {
    if (errorCode(error) !== reauthRequiredCode) {
      return false
    }
    setPending({ action: null })
    return true
  }
  const panel =
    pending && credentials ? (
      <VerifyPanel
        credentials={credentials}
        repeat={pending.action === null}
        onVerified={(until) => {
          queryClient.setQueryData<Credentials>(credentialsQueryKey, (old) =>
            old ? { ...old, verifiedUntil: until } : old,
          )
          const action = pending.action
          setPending(null)
          action?.()
        }}
        onCancel={() => {
          setPending(null)
        }}
      />
    ) : null
  return { guard, handleError, panel }
}
