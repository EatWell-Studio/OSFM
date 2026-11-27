import { useTranslation } from 'react-i18next'

/** The top bar: the instance's name, once known (instance settings), else a placeholder. */
export function TopBar({ name }: { name?: string }) {
  const { t } = useTranslation()
  return (
    <header className="sticky top-0 z-10 border-b bg-background pt-[env(safe-area-inset-top)]">
      <div className="flex h-12 items-center px-4">
        <span className="truncate font-semibold">
          {name !== undefined && name !== '' ? name : t('app.instanceNamePlaceholder')}
        </span>
      </div>
    </header>
  )
}
