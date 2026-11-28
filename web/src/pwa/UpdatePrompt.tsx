import { useTranslation } from 'react-i18next'
import { useRegisterSW } from 'virtual:pwa-register/react'
import { Button } from '@/components/ui/button'

/** How often an app left open (an installed one, for days) asks the server whether a new version exists. */
const updateCheckMillis = 60 * 60 * 1000

/** The banner that offers the new version: shown while one is waiting, on every page. */
export function UpdateBanner({ onReload }: { onReload: () => void }) {
  const { t } = useTranslation()
  return (
    <div
      role="status"
      className="fixed inset-x-0 top-0 z-50 flex items-center justify-between gap-3 border-b bg-background px-4 pt-[env(safe-area-inset-top)] shadow-sm"
    >
      <p className="py-2 text-sm">{t('app.update.available')}</p>
      <Button size="sm" onClick={onReload}>
        {t('app.update.reload')}
      </Button>
    </div>
  )
}

/**
 * Registers the service worker (src/sw/sw.ts; docs/architecture.md §4.5) and, when a new version of the app has been
 * installed and waits, offers to reload into it. Until then the open pages keep the version they started with.
 */
export function UpdatePrompt() {
  const {
    needRefresh: [needRefresh],
    updateServiceWorker,
  } = useRegisterSW({
    onRegisteredSW(_url, registration) {
      if (!registration) {
        return
      }
      const check = () => {
        if (document.visibilityState === 'visible' && navigator.onLine) {
          void registration.update().catch(() => undefined)
        }
      }
      setInterval(check, updateCheckMillis)
      document.addEventListener('visibilitychange', check)
    },
  })
  if (!needRefresh) {
    return null
  }
  return (
    <UpdateBanner
      onReload={() => {
        void updateServiceWorker(true)
      }}
    />
  )
}
