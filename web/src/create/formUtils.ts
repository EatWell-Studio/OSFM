// Hooks and helpers of the create flow's forms (the components are in controls.tsx and RecommendationFields.tsx).

import { useQuery } from '@tanstack/react-query'
import { useSyncExternalStore } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { hasPseudonym, profileQuery } from '@/me/profileApi'
import type { Problem } from './recommendationForm'

/** The text of a problem, with the field's limit where it has one. */
export function useProblemText(): (problem: Problem | undefined, max?: number) => string | null {
  const { t } = useTranslation('create')
  return (problem, max) => (problem ? t(`errors.${problem}`, { max: max ?? 0 }) : null)
}

/** The id of a field's problem text, for aria-describedby. */
export const problemId = (id: string) => `${id}-problem`

export const inputClass =
  'w-full min-w-0 rounded-lg border bg-background px-3 text-base outline-none focus-visible:border-ring ' +
  'focus-visible:ring-3 focus-visible:ring-ring/50 aria-invalid:border-destructive'

/** The class of the form's one-line inputs and native selects. */
export const selectClass = cn(inputClass, 'h-11')

/** Whether the member can sign with a pseudonym: they have one (their profile says so once loaded, C3). */
export function useCanUsePseudonym(): boolean {
  const profile = useQuery(profileQuery)
  return profile.data ? hasPseudonym(profile.data) : false
}

/** Focuses the first field of the form with a problem, once React has drawn the problems. */
export function focusFirstProblem(form: HTMLFormElement | null) {
  setTimeout(() => {
    form?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
  }, 0)
}

function subscribe(onChange: () => void) {
  window.addEventListener('online', onChange)
  window.addEventListener('offline', onChange)
  return () => {
    window.removeEventListener('online', onChange)
    window.removeEventListener('offline', onChange)
  }
}

/** Whether the browser says it is online (navigator.onLine, kept up to date). */
export function useOnline(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  )
}
