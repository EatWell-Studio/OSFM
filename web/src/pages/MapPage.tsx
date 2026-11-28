import { lazy, Suspense } from 'react'
import { useTranslation } from 'react-i18next'
import { useRememberMapSearch } from '@/filters/useFilters'

// MapLibre is most of the map's weight; the shell loads it only when the map is shown.
const MapView = lazy(() => import('@/map/MapView'))

/**
 * The map, filling the space between the top bar (3rem and its 1px border) and the bottom navigation (3.5rem and its
 * border), so that nothing covers its attribution.
 */
export function MapPage() {
  const { t } = useTranslation('map')
  // The city and filters of the map, for the navigation's way back from other pages.
  useRememberMapSearch()
  return (
    <div className="h-[calc(100dvh-6.5rem-2px-env(safe-area-inset-top)-env(safe-area-inset-bottom))]">
      <h1 className="sr-only">{t('title')}</h1>
      <Suspense
        fallback={
          <div role="status" className="flex h-full items-center justify-center text-sm">
            {t('loading')}
          </div>
        }
      >
        <MapView />
      </Suspense>
    </div>
  )
}
