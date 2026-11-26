import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router'
import { ApiError, apiFetch } from '@/lib/api'
import { loginPath } from '@/lib/next'
import { sessionQueryKey } from './session'

/**
 * Signs out: POST /api/osfm/logout ends this device's session (the member's other devices stay signed in), then the
 * app forgets everything it fetched and goes to the sign-in page, without a `next` to return to.
 */
export function useLogout() {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  return useMutation({
    mutationFn: async () => {
      const response = await apiFetch('/logout', { method: 'POST' })
      // 401: the session had ended already; the server has told the browser to drop the cookie either way.
      if (!response.ok && response.status !== 401) {
        throw new ApiError(response.status, '/logout')
      }
    },
    onSuccess: () => {
      queryClient.removeQueries()
      queryClient.setQueryData(sessionQueryKey, null)
      void navigate(loginPath, { replace: true })
    },
  })
}
