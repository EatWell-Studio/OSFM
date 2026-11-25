import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import type { Session } from './useSession'

/** What to show while the session is unknown: loading, or an error with a retry button. */
export function SessionPending({ session }: { session: Extract<Session, { status: 'loading' | 'error' }> }) {
  const { t } = useTranslation()
  if (session.status === 'loading') {
    return (
      <p role="status" className="p-4 text-muted-foreground">
        {t('app.loading')}
      </p>
    )
  }
  return (
    <div role="alert" className="flex flex-col items-start gap-3 p-4">
      <p>{t('session.error')}</p>
      <Button onClick={session.retry}>{t('session.retry')}</Button>
    </div>
  )
}
