import { type QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { I18nextProvider } from 'react-i18next'
import { type DataRouter, RouterProvider } from 'react-router'
import { i18n } from '@/i18n'

/** The app with its providers. The router and query client come from outside so that tests can supply their own. */
export function App({ router, queryClient }: { router: DataRouter; queryClient: QueryClient }) {
  return (
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nextProvider>
  )
}
