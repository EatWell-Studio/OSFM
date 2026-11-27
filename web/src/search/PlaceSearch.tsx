// The place search box (B7): a search term goes to this instance (searchPlaces), which asks the search services; the
// member picks a result and the page moves its map there. Self-contained, so that every page with a map can put it
// above the map without changing this file: the map page, the pin step of a new place, the list.
//
//   <PlaceSearch city={citySlug} onSelect={(r) => map.flyTo({ center: [r.lon, r.lat], zoom: 16 })} />
//
// It searches only when the member submits (Enter or the button), never as they type: the usage policy of Nominatim
// forbids search as you type. It renders nothing when the instance says search is off, or for a city the instance
// does not have. A failed search shows a message
// that the map can still be moved by hand; it never blocks placing a pin.

import { useMutation, useQuery } from '@tanstack/react-query'
import { SearchIcon } from 'lucide-react'
import { cn } from 'cn'
import { type SyntheticEvent, useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { instanceQuery } from '@/instance/instance'
import { retryAfterMinutes } from '@/lib/rateLimit'
import { type GeocodeResult, maxQueryLength, SearchError, searchPlaces } from './geocodeApi'

export type { GeocodeResult }

export interface PlaceSearchProps {
  /** The slug of the city to search in: the city the map shows. Results lie inside it. */
  city: string
  /** Called with the result the member picked; the page moves its map there. */
  onSelect: (result: GeocodeResult) => void
  /** Classes for the outer element, for placing it (it takes the full width it is given). */
  className?: string
}

/** The search box with its results and messages; nothing when search is off. */
export function PlaceSearch({ city, onSelect, className }: PlaceSearchProps) {
  const { t } = useTranslation('search')
  const id = useId()
  const instance = useQuery(instanceQuery)
  const [text, setText] = useState('')
  // Set once a search answers that search is off (the instance settings may be older than that).
  const [turnedOff, setTurnedOff] = useState(false)
  const search = useMutation({
    mutationFn: (query: string) => searchPlaces(query, city),
    onError: (error) => {
      if (error instanceof SearchError && error.failure.kind === 'disabled') {
        setTurnedOff(true)
      }
    },
  })
  const { reset } = search

  // Another city: the results of the last one do not apply.
  useEffect(() => {
    reset()
  }, [city, reset])

  // Off, or a city the instance does not have (a basemap archive alone): the server would refuse the search.
  const known = instance.data?.cities.some((c) => c.slug === city) ?? true
  if (instance.data?.geocoderEnabled === false || turnedOff || !known) {
    return null
  }

  const submit = (event: SyntheticEvent) => {
    event.preventDefault()
    const query = text.trim()
    if (query !== '' && !search.isPending) {
      search.mutate(query)
    }
  }

  const pick = (result: GeocodeResult) => {
    reset()
    onSelect(result)
  }

  let message: string | null = null
  if (search.isPending) {
    message = t('searching')
  } else if (search.isSuccess && search.data.length === 0) {
    message = t('noResults')
  } else if (search.isError) {
    const failure = search.error instanceof SearchError ? search.error.failure : null
    message =
      failure?.kind === 'rateLimited'
        ? t('rateLimited', { count: failure.retryAfter === null ? 1 : retryAfterMinutes(failure.retryAfter) })
        : t('unavailable')
  }
  const results = search.isSuccess ? search.data : []

  return (
    <div className={cn('relative', className)}>
      <form role="search" aria-label={t('title')} onSubmit={submit} className="flex gap-1">
        <label htmlFor={`${id}-q`} className="sr-only">
          {t('label')}
        </label>
        <Input
          id={`${id}-q`}
          type="search"
          enterKeyHint="search"
          autoComplete="off"
          maxLength={maxQueryLength}
          placeholder={t('placeholder')}
          value={text}
          onChange={(event) => {
            setText(event.target.value)
            if (!search.isPending) {
              reset()
            }
          }}
          onKeyDown={(event) => {
            if (event.key === 'Escape') {
              reset()
            }
          }}
          className="h-9 bg-background shadow-sm dark:bg-background"
        />
        <Button
          type="submit"
          variant="outline"
          size="icon"
          aria-label={t('submit')}
          disabled={search.isPending}
          className="size-9 bg-background shadow-sm"
        >
          <SearchIcon aria-hidden="true" />
        </Button>
      </form>

      {results.length > 0 && (
        <ul
          aria-label={t('results')}
          className="absolute inset-x-0 top-full z-10 mt-1 max-h-72 overflow-y-auto rounded-md border bg-background shadow-md"
        >
          {results.map((r) => (
            <li key={`${r.name} ${String(r.lat)} ${String(r.lon)}`}>
              <button
                type="button"
                onClick={() => {
                  pick(r)
                }}
                className="flex w-full flex-col items-start px-3 py-2 text-left text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none"
              >
                <span className="font-medium">{r.name}</span>
                {r.label !== '' && <span className="text-xs text-muted-foreground">{r.label}</span>}
              </button>
            </li>
          ))}
        </ul>
      )}

      <div role="status" aria-live="polite">
        {message && (
          <p className="mt-1 rounded-md bg-foreground/85 px-3 py-2 text-sm text-background shadow">{message}</p>
        )}
      </div>
    </div>
  )
}
