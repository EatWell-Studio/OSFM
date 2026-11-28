import { lazy, Suspense } from 'react'
import { useTranslation } from 'react-i18next'
import { Outlet } from 'react-router'

// MapLibre is most of the map's weight; the shell loads it only when the map is shown.
const MapView = lazy(() => import('@/map/MapView'))

/**
 * The map, filling the space between the top bar (3rem and its 1px border) and the bottom navigation (3.5rem and its
 * border), so that nothing covers its attribution. A place's card (the child route /p/:place, T029) takes the lower
 * part of the screen on phones and a column on the left on wide screens; the map shrinks to the rest rather than lying
 * under it, so that the place and the attribution stay visible.
 */
export function MapPage() {
  const { t } = useTranslation('map')
  return (
    <div className="flex h-[calc(100dvh-6.5rem-2px-env(safe-area-inset-top)-env(safe-area-inset-bottom))] flex-col md:flex-row">
      <h1 className="sr-only">{t('title')}</h1>
      <div className="relative min-h-0 min-w-0 flex-1">
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
      <Outlet />
    </div>
  )
}
