import { useCallback, useEffect, useMemo } from 'react'
import { useLocation, useSearchParams } from 'react-router'
import { listPath } from '@/lib/paths'
import { type Filters, mapSearch, parseFilters, writeFilters } from './filters'

/**
 * The filters of the current address and a function that changes them. A change replaces the history entry (as the
 * city does), so that the back button leaves the map instead of stepping through every tap on a filter.
 */
export function useFilters(): [Filters, (filters: Filters) => void] {
  const [searchParams, setSearchParams] = useSearchParams()
  const key = searchParams.toString()
  // eslint-disable-next-line react-hooks/exhaustive-deps -- recomputed when the query string changes, not the object
  const filters = useMemo(() => parseFilters(searchParams), [key])
  const setFilters = useCallback(
    (next: Filters) => {
      setSearchParams((params) => writeFilters(params, next), { replace: true })
    },
    [setSearchParams],
  )
  return [filters, setFilters]
}

// The query string of the map or the list last shown (city and filters), for the navigation's links from other pages:
// coming back from "me" shows the map as it was left. Only in memory, for this page load.
let rememberedSearch = ''

/** Remembers the current page's map query string (call on the map and the list). Returns it. */
export function useRememberMapSearch(): string {
  const { search } = useLocation()
  const own = mapSearch(new URLSearchParams(search))
  useEffect(() => {
    rememberedSearch = own
  }, [own])
  return own
}

/** Whether the path shows the map's places: the map, the list, or a place opened from them (/p/…). */
function showsMapQuery(pathname: string): boolean {
  return pathname === '/' || pathname === listPath || pathname.startsWith('/p/')
}

/**
 * The query string for links to the map and the list: the current page's own on the map, the list and a place, else
 * the one last remembered.
 */
export function useMapSearch(): string {
  const { pathname, search } = useLocation()
  return showsMapQuery(pathname) ? mapSearch(new URLSearchParams(search)) : rememberedSearch
}
