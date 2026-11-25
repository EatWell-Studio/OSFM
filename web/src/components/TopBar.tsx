import { useTranslation } from 'react-i18next'

/** The top bar. The instance name is a placeholder until the instance settings exist (setup wizard). */
export function TopBar() {
  const { t } = useTranslation()
  return (
    <header className="sticky top-0 z-10 border-b bg-background pt-[env(safe-area-inset-top)]">
      <div className="flex h-12 items-center px-4">
        <span className="font-semibold">{t('app.instanceNamePlaceholder')}</span>
      </div>
    </header>
  )
}
