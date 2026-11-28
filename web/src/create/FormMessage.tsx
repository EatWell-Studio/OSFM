// The messages above a create form's save button, and whether the browser is online: new places and recommendations
// need the connection (docs/architecture.md §4.5), and the pages say so instead of failing.

import { useTranslation } from 'react-i18next'
import { rateLimitMessage } from '@/lib/rateLimit'
import { WriteError } from './createApi'
import { useOnline } from './formUtils'
import type { Problems } from './recommendationForm'

/** "Saving needs the connection", while the browser is offline. */
export function OfflineNotice() {
  const { t } = useTranslation('create')
  const online = useOnline()
  if (online) {
    return null
  }
  return (
    <p role="status" className="rounded-md bg-foreground/85 px-3 py-2 text-sm text-background">
      {t('errors.offline')}
    </p>
  )
}

/**
 * What stops the save: a summary when fields have problems (each is shown at its field), the position outside the
 * cities, or an error that is about no field (no connection, too many saves, the server). Nothing otherwise.
 */
export function FormMessage({ problems, error }: { problems: Problems; error: unknown }) {
  const { t } = useTranslation('create')
  let message: string | null = null
  if (problems.position) {
    message = t('errors.outsideCities')
  } else if (problems.form) {
    message = t('errors.form')
  } else if (Object.keys(problems).length > 0) {
    message = t('errors.summary')
  } else if (error instanceof WriteError) {
    if (error.status === 0) {
      message = navigator.onLine ? t('errors.network') : t('errors.offline')
    } else if (error.status === 429) {
      message = rateLimitMessage(error.retryAfter)
    } else if (error.status === 404) {
      message = t('errors.notFound')
    } else if (error.status !== 401 && error.status !== 409) {
      message = t('errors.server')
    }
  } else if (error) {
    message = t('errors.server')
  }
  return (
    <div role="alert" className="empty:hidden">
      {message && <p className="text-sm text-destructive">{message}</p>}
    </div>
  )
}
