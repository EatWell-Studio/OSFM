import { type QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { type DataRouter, RouterProvider } from 'react-router'
import { i18n } from '@/i18n'

/**
 * The app with its providers. The router and query client come from outside so that tests can supply their own;
 * children (the service worker's update prompt) are rendered beside the router, within the providers.
 */
export function App({
  router,
  queryClient,
  children,
}: {
  router: DataRouter
  queryClient: QueryClient
  children?: ReactNode
}) {
  return (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
        {children}
      </QueryClientProvider>
    </I18nextProvider>
  )
}
