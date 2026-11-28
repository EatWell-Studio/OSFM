// The recommendation form's values, checks and request (C2, C3, C4): shared by a new place (/new), a recommendation of
// a place that exists (/p/:place/recommend) and editing one (T031). The checks repeat the server's
// (domain.NormalizeRecommendation) so that most mistakes show before saving; the server stays the judge, and its
// field errors map onto the same fields (serverProblems).

import type { RecommendationInput, RecommendationView } from '@/api/types.gen'
import {
  type Attitude,
  attitudes,
  type Signature,
  type SourceKind,
  sourceKinds,
  type TimeSlot,
  timeSlots,
  type Visibility,
  visibilities,
} from '@/lib/enums'
import { WriteError } from './createApi'

/** The server's limits (internal/domain: MaxTextLength and the others), in characters unless said otherwise. */
export const limits = {
  name: 200,
  positionNote: 500,
  landmark: 200,
  text: 5000,
  dishes: 20,
  dish: 100,
  season: 100,
  sources: 10,
  /** bytes, as the server counts them */
  sourceUrl: 2000,
  excerpt: 500,
} as const

/** A source as the form holds it; key only tells the rows apart. */
export interface SourceValues {
  key: number
  url: string
  kind: SourceKind
  excerpt: string
}

/** The recommendation as the form holds it. attitude is null until the member picks one: there is no default (C2). */
export interface RecommendationValues {
  attitude: Attitude | null
  text: string
  timeSlots: TimeSlot[]
  dishes: string[]
  season: string
  sources: SourceValues[]
  /** YYYY-MM-DD or "" */
  visitedOn: string
  displayAs: Signature
  visibility: Visibility
}

export const emptyRecommendation: RecommendationValues = {
  attitude: null,
  text: '',
  timeSlots: [],
  dishes: [],
  season: '',
  sources: [],
  visitedOn: '',
  displayAs: 'main',
  visibility: 'circle',
}

let nextSourceKey = 0

/** A new, empty source row. */
export function newSource(kind: SourceKind = 'visited'): SourceValues {
  return { key: nextSourceKey++, url: '', kind, excerpt: '' }
}

const isAttitude = (v: string): v is Attitude => (attitudes as readonly string[]).includes(v)
const isTimeSlot = (v: string): v is TimeSlot => (timeSlots as readonly string[]).includes(v)
const isSourceKind = (v: string): v is SourceKind => (sourceKinds as readonly string[]).includes(v)
const isVisibility = (v: string): v is Visibility => (visibilities as readonly string[]).includes(v)

/**
 * The form's values for a recommendation as the card shows it, for editing (T031). The card does not say which name a
 * recommendation is signed with (it cannot be changed, M2-Q2): displayAs is "main" here and is not sent when the
 * signature is fixed (toRecommendationInput).
 */
export function recommendationValuesFrom(view: RecommendationView): RecommendationValues {
  return {
    attitude: isAttitude(view.attitude) ? view.attitude : null,
    text: view.text,
    timeSlots: view.timeSlots.filter(isTimeSlot),
    dishes: [...view.dishes],
    season: view.season,
    sources: view.sources.map((s) => ({
      ...newSource(isSourceKind(s.kind) ? s.kind : 'other'),
      url: s.url ?? '',
      excerpt: s.excerpt ?? '',
    })),
    visitedOn: view.visitedOn ?? '',
    displayAs: 'main',
    visibility: view.visibility !== undefined && isVisibility(view.visibility) ? view.visibility : 'circle',
  }
}

/** What is wrong with a field; the form shows it with the field's limit where there is one. */
export type Problem =
  | 'required'
  | 'tooLong'
  | 'tooMany'
  | 'oneLine'
  | 'invalidUrl'
  | 'invalidDate'
  | 'futureDate'
  | 'noPseudonym'
  | 'outsideCities'
  | 'invalid'

/**
 * The problems of a form by field: attitude, text, timeSlots, dishes, season, sources, sources.<i>.url,
 * sources.<i>.kind, sources.<i>.excerpt, visitedOn, displayAs, visibility; a new place adds name, precision, category,
 * cuisine, positionNote, landmark and position (the crosshair's point); form is about the whole form.
 */
export type Problems = Partial<Record<string, Problem>>

/** The number of characters as the server counts them (code points). */
export function length(s: string): number {
  return Array.from(s).length
}

/** Whether s, trimmed, fits on one line without control characters and has at most max characters. */
export function lineProblem(s: string, max: number, required: boolean): Problem | null {
  const trimmed = s.trim()
  if (trimmed === '') {
    return required ? 'required' : null
  }
  if (length(trimmed) > max) {
    return 'tooLong'
  }
  // eslint-disable-next-line no-control-regex -- control characters are what this checks for
  return /[\u0000-\u001f\u007f-\u009f]/u.test(trimmed) ? 'oneLine' : null
}

/** Whether s, trimmed, has at most max characters (and is not empty when required); it may span lines. */
export function textProblem(s: string, max: number, required: boolean): Problem | null {
  const trimmed = s.trim()
  if (trimmed === '') {
    return required ? 'required' : null
  }
  return length(trimmed) > max ? 'tooLong' : null
}

/** Whether url is an absolute http(s) link with a host and without credentials, as the server takes them. */
export function isWebUrl(url: string): boolean {
  if (new TextEncoder().encode(url).length > limits.sourceUrl || /\s/u.test(url)) {
    return false
  }
  try {
    const parsed = new URL(url)
    return (
      (parsed.protocol === 'http:' || parsed.protocol === 'https:') &&
      parsed.hostname !== '' &&
      parsed.username === '' &&
      parsed.password === ''
    )
  } catch {
    return false
  }
}

/** Today in the member's time zone, YYYY-MM-DD: the latest visit date the form accepts. */
export function today(now: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${String(now.getFullYear())}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`
}

/** The dishes as they are sent: trimmed, without empty ones and repeats. */
export function cleanDishes(dishes: readonly string[]): string[] {
  return [...new Set(dishes.map((d) => d.trim()).filter((d) => d !== ''))]
}

/**
 * The problems of the recommendation's values, none when it can be sent. canUsePseudonym says whether the member has a
 * pseudonym to sign with (C3); signatureFixed that the signature is not part of the form (editing, M2-Q2).
 */
export function recommendationProblems(
  values: RecommendationValues,
  options: { canUsePseudonym: boolean; signatureFixed?: boolean; today?: string },
): Problems {
  const problems: Problems = {}
  if (values.attitude === null) {
    problems.attitude = 'required'
  }
  const text = textProblem(values.text, limits.text, true)
  if (text) {
    problems.text = text
  }
  const dishes = cleanDishes(values.dishes)
  if (dishes.length > limits.dishes) {
    problems.dishes = 'tooMany'
  } else if (dishes.some((d) => lineProblem(d, limits.dish, false) !== null)) {
    problems.dishes = 'tooLong'
  }
  const season = lineProblem(values.season, limits.season, false)
  if (season) {
    problems.season = season
  }
  if (values.sources.length > limits.sources) {
    problems.sources = 'tooMany'
  }
  values.sources.forEach((s, i) => {
    const url = s.url.trim()
    if (url !== '' && !isWebUrl(url)) {
      problems[`sources.${String(i)}.url`] = 'invalidUrl'
    }
    const excerpt = textProblem(s.excerpt, limits.excerpt, false)
    if (excerpt) {
      problems[`sources.${String(i)}.excerpt`] = excerpt
    }
  })
  const visited = values.visitedOn.trim()
  if (visited !== '') {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(visited) || Number.isNaN(Date.parse(visited)) || visited < '1900-01-01') {
      problems.visitedOn = 'invalidDate'
    } else if (visited > (options.today ?? today())) {
      problems.visitedOn = 'futureDate'
    }
  }
  if (!options.signatureFixed && values.displayAs === 'pseudonym' && !options.canUsePseudonym) {
    problems.displayAs = 'noPseudonym'
  }
  return problems
}

/**
 * The request for the values, which recommendationProblems found nothing wrong with. Optional fields left empty are
 * not sent. Without the signature (editing: it cannot be changed, M2-Q2) or the visibility (a maintainer editing
 * someone else's) when the form does not offer them.
 */
export function toRecommendationInput(
  values: RecommendationValues,
  options: { signature?: boolean; visibility?: boolean } = {},
): RecommendationInput {
  if (values.attitude === null) {
    throw new Error('toRecommendationInput: no attitude')
  }
  const input: RecommendationInput = { attitude: values.attitude, text: values.text.trim() }
  const dishes = cleanDishes(values.dishes)
  if (dishes.length > 0) {
    input.dishes = dishes
  }
  if (values.timeSlots.length > 0) {
    input.timeSlots = timeSlots.filter((s) => values.timeSlots.includes(s))
  }
  if (values.season.trim() !== '') {
    input.season = values.season.trim()
  }
  if (values.sources.length > 0) {
    input.sources = values.sources.map((s) => {
      const source: { kind: string; url?: string; excerpt?: string } = { kind: s.kind }
      if (s.url.trim() !== '') {
        source.url = s.url.trim()
      }
      if (s.excerpt.trim() !== '') {
        source.excerpt = s.excerpt.trim()
      }
      return source
    })
  }
  if (values.visitedOn.trim() !== '') {
    input.visitedOn = values.visitedOn.trim()
  }
  if (options.signature !== false) {
    input.displayAs = values.displayAs
  }
  if (options.visibility !== false) {
    input.visibility = values.visibility
  }
  return input
}

/**
 * The form field a field of the server's invalid_request is about (its name in the request, InvalidField): the
 * recommendation's fields without prefix ("recommendation." on a new place), list entries on their list
 * ("dishes[2]" → dishes), a source's parts on that source ("sources[1].url" → sources.1.url), the coordinates on the
 * position. Anything else is about the whole form.
 */
export function formField(serverField: string, prefix = ''): string {
  let name = serverField
  if (prefix !== '' && name.startsWith(prefix)) {
    name = name.slice(prefix.length)
  }
  const source = /^sources\[(\d+)\]\.(url|kind|excerpt)$/.exec(name)
  if (source) {
    return `sources.${source[1] ?? '0'}.${source[2] ?? 'url'}`
  }
  const base = name.replace(/\[\d+\]$/, '').replace(/\..*$/, '')
  const known = [
    'attitude',
    'text',
    'dishes',
    'timeSlots',
    'season',
    'sources',
    'visitedOn',
    'displayAs',
    'visibility',
    'name',
    'precision',
    'positionNote',
    'landmark',
    'category',
    'cuisine',
  ]
  if (base === 'lat' || base === 'lon' || base === 'sourceCoord') {
    return 'position'
  }
  return known.includes(base) ? base : 'form'
}

/**
 * The problems a refused write is about, by form field; null when the error is not about the form's values (offline,
 * rate limits, already recommended, a missing place, server errors: the page says those).
 */
export function serverProblems(error: unknown, prefix = ''): Problems | null {
  if (!(error instanceof WriteError) || error.status !== 400) {
    return null
  }
  switch (error.code) {
    case 'outside_cities':
      return { position: 'outsideCities' }
    case 'no_pseudonym':
      return { displayAs: 'noPseudonym' }
    case 'invalid_request':
      return error.field ? { [formField(error.field, prefix)]: 'invalid' } : { form: 'invalid' }
    default:
      return { form: 'invalid' }
  }
}
