// A place's card at its own address, /p/:place, which is also the share link (M2-Q15). It is a child route of the map
// page: the card takes the lower part of the screen on phones and a column on wide screens, beside the map, which stays
// as it was. Opening the card switches the map to the place's city (A7, through the map's ?city=) and the map centres
// the place (useCentreOnPlace). Closing it goes back to the map with the same query string.

import { useQuery } from '@tanstack/react-query'
import { XIcon } from 'lucide-react'
import { type ReactNode, useEffect, useId, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router'
import { Button } from '@/components/ui/button'
import { useMapSearch } from '@/filters/useFilters'
import { usePlaceParams } from '@/lib/paths'
import { cityParam, readRememberedCity } from '@/map/rememberedCity'
import { PlaceCard } from './PlaceCard'
import { isPlaceNotFound, placeCardQuery } from './placeApi'

export function PlacePage() {
  const { t } = useTranslation('common')
  const params = usePlaceParams()
  if (!params) {
    return (
      <Drawer>
        <p role="alert" className="pr-10 text-muted-foreground">
          {t('invalidLink')}
        </p>
      </Drawer>
    )
  }
  return <PlaceDrawer place={params.place} />
}

function PlaceDrawer({ place }: { place: string }) {
  const { t } = useTranslation(['place', 'common'])
  const card = useQuery(placeCardQuery(place))
  const headingId = useId()
  const headingRef = useRef<HTMLHeadingElement>(null)
  const [searchParams, setSearchParams] = useSearchParams()
  const city = card.data?.uuid === place ? card.data.city : undefined

  // Once per place: show its city on the map (A7). Afterwards the member may switch cities with the card open.
  const switched = useRef<string | null>(null)
  useEffect(() => {
    if (city === undefined || switched.current === place) {
      return
    }
    switched.current = place
    // The map shows the city of the address, else the one it showed last (src/map/MapView.tsx); only another city is
    // set, so that the address stays as it was when the map shows the place's city already.
    const shown = searchParams.get(cityParam) ?? readRememberedCity()
    if (city !== '' && shown !== city) {
      setSearchParams(
        (p) => {
          p.set(cityParam, city)
          return p
        },
        { replace: true },
      )
    }
  }, [place, city, searchParams, setSearchParams])

  // The card's heading takes the focus when a place opens, so that keyboard and screen reader users start there.
  const loaded = card.isSuccess
  useEffect(() => {
    if (loaded) {
      headingRef.current?.focus({ preventScroll: true })
    }
  }, [loaded, place])

  let content: ReactNode
  if (card.isPending) {
    content = (
      <p role="status" className="pr-10 text-sm text-muted-foreground">
        {t('loading')}
      </p>
    )
  } else if (card.isError) {
    content = isPlaceNotFound(card.error) ? (
      <h2 id={headingId} ref={headingRef} tabIndex={-1} className="pr-10 text-xl font-semibold outline-none">
        {t('notFound')}
      </h2>
    ) : (
      <div role="alert" className="flex flex-col items-start gap-3 pr-10 text-sm">
        <p>{t('loadError')}</p>
        <Button
          variant="outline"
          onClick={() => {
            void card.refetch()
          }}
        >
          {t('common:retry')}
        </Button>
      </div>
    )
  } else {
    content = <PlaceCard place={card.data} headingId={headingId} headingRef={headingRef} />
  }
  return <Drawer labelledBy={card.isError || card.isSuccess ? headingId : undefined}>{content}</Drawer>
}

/** The drawer around the card: a close button, Escape to close, its own scrolling. */
function Drawer({ labelledBy, children }: { labelledBy?: string; children: ReactNode }) {
  const { t } = useTranslation('place')
  const navigate = useNavigate()
  // Back to the map with its city and filters, without the share link's ?r= (T028).
  const search = useMapSearch()
  const close = () => {
    void navigate({ pathname: '/', search })
  }
  return (
    <aside
      aria-labelledby={labelledBy}
      aria-label={labelledBy ? undefined : t('title')}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          close()
        }
      }}
      className="relative flex max-h-[60%] min-h-0 shrink-0 flex-col border-t bg-background md:order-first md:max-h-none md:w-96 md:border-t-0 md:border-r"
    >
      <Button
        variant="ghost"
        size="icon"
        aria-label={t('close')}
        onClick={close}
        className="absolute top-2 right-2 z-[1]"
      >
        <XIcon aria-hidden="true" />
      </Button>
      <div className="min-h-0 overflow-y-auto overscroll-contain p-4 pb-6">{children}</div>
    </aside>
  )
}
