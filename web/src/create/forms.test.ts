import { describe, expect, it } from 'vitest'
import type { RecommendationView } from '@/api/types.gen'
import { WriteError } from './createApi'
import { emptyPlace, newPlaceProblems, type PlaceValues, toCreatePlaceRequest } from './placeForm'
import {
  emptyRecommendation,
  formField,
  newSource,
  recommendationProblems,
  recommendationValuesFrom,
  type RecommendationValues,
  serverProblems,
  toRecommendationInput,
} from './recommendationForm'

// Made-up data only.
// One character (code point) that is two UTF-16 units and four UTF-8 bytes: the server counts characters.
const wide = '\u{1F35C}'
const minimal: RecommendationValues = { ...emptyRecommendation, attitude: 'good', text: 'Test one sentence' }
const point = { lat: 0.34, lon: 7.06, source: 'crosshair' as const }
const options = { canUsePseudonym: false, today: '2026-11-28' }

describe('the recommendation checks', () => {
  it('need an attitude and a text, and nothing else', () => {
    expect(recommendationProblems(emptyRecommendation, options)).toEqual({ attitude: 'required', text: 'required' })
    expect(recommendationProblems(minimal, options)).toEqual({})
  })

  it('take a text of blanks as empty and count characters, not bytes', () => {
    expect(recommendationProblems({ ...minimal, text: '  \n\t ' }, options).text).toBe('required')
    expect(recommendationProblems({ ...minimal, text: wide.repeat(5000) }, options).text).toBeUndefined()
    expect(recommendationProblems({ ...minimal, text: wide.repeat(5001) }, options).text).toBe('tooLong')
    expect(recommendationProblems({ ...minimal, text: `  ${'a'.repeat(5000)}  ` }, options).text).toBeUndefined()
  })

  it('limit the dishes, the season and the sources', () => {
    const dishes = Array.from({ length: 21 }, (_, i) => `Test Dish ${String(i)}`)
    expect(recommendationProblems({ ...minimal, dishes }, options).dishes).toBe('tooMany')
    // Repeats and empty entries do not count.
    expect(
      recommendationProblems({ ...minimal, dishes: [...dishes.slice(0, 20), ' ', dishes[0] ?? ''] }, options),
    ).toEqual({})
    expect(recommendationProblems({ ...minimal, dishes: ['x'.repeat(101)] }, options).dishes).toBe('tooLong')
    expect(recommendationProblems({ ...minimal, season: 'a\nb' }, options).season).toBe('oneLine')
    const sources = Array.from({ length: 11 }, () => newSource())
    expect(recommendationProblems({ ...minimal, sources }, options).sources).toBe('tooMany')
  })

  it('accept only http(s) links in sources, and short excerpts', () => {
    const sources = [
      { ...newSource('chat'), url: 'https://example.org/test' },
      { ...newSource(), url: 'javascript:alert(1)' },
      { ...newSource(), url: 'example.org/no-scheme' },
      { ...newSource(), url: 'https://user:pw@example.org/' },
      { ...newSource(), excerpt: 'x'.repeat(501) },
    ]
    expect(recommendationProblems({ ...minimal, sources }, options)).toEqual({
      'sources.1.url': 'invalidUrl',
      'sources.2.url': 'invalidUrl',
      'sources.3.url': 'invalidUrl',
      'sources.4.excerpt': 'tooLong',
    })
  })

  it('refuse a visit date after today and dates that are not dates', () => {
    expect(recommendationProblems({ ...minimal, visitedOn: '2026-11-28' }, options)).toEqual({})
    expect(recommendationProblems({ ...minimal, visitedOn: '2026-11-29' }, options).visitedOn).toBe('futureDate')
    expect(recommendationProblems({ ...minimal, visitedOn: '1899-12-31' }, options).visitedOn).toBe('invalidDate')
    expect(recommendationProblems({ ...minimal, visitedOn: '28.11.2026' }, options).visitedOn).toBe('invalidDate')
  })

  it('offer the pseudonym only to a member who has one, unless the signature is fixed', () => {
    const pen = { ...minimal, displayAs: 'pseudonym' as const }
    expect(recommendationProblems(pen, options).displayAs).toBe('noPseudonym')
    expect(recommendationProblems(pen, { ...options, canUsePseudonym: true })).toEqual({})
    expect(recommendationProblems(pen, { ...options, signatureFixed: true })).toEqual({})
  })
})

describe('the recommendation request', () => {
  it('has only the attitude, the text, the signature and the visibility on the shortest path', () => {
    expect(toRecommendationInput({ ...minimal, text: '  Test one sentence \n' })).toEqual({
      attitude: 'good',
      text: 'Test one sentence',
      displayAs: 'main',
      visibility: 'circle',
    })
  })

  it('sends the optional fields cleaned up, time slots in their order', () => {
    const values: RecommendationValues = {
      ...minimal,
      timeSlots: ['dinner', 'breakfast'],
      dishes: [' Test Dish ', '', 'Test Dish', 'Test Soup'],
      season: ' winter ',
      sources: [
        { ...newSource('chat'), url: ' https://example.org/test ', excerpt: ' Test quote ' },
        { ...newSource('visited') },
      ],
      visitedOn: '2026-11-01',
      displayAs: 'pseudonym',
      visibility: 'private',
    }
    expect(toRecommendationInput(values)).toEqual({
      attitude: 'good',
      text: 'Test one sentence',
      timeSlots: ['breakfast', 'dinner'],
      dishes: ['Test Dish', 'Test Soup'],
      season: 'winter',
      sources: [{ kind: 'chat', url: 'https://example.org/test', excerpt: 'Test quote' }, { kind: 'visited' }],
      visitedOn: '2026-11-01',
      displayAs: 'pseudonym',
      visibility: 'private',
    })
  })

  it('leaves out the signature and the visibility when the form does not offer them (editing)', () => {
    expect(toRecommendationInput(minimal, { signature: false, visibility: false })).toEqual({
      attitude: 'good',
      text: 'Test one sentence',
    })
  })

  it('starts editing from the card (T031) without the signature, which the card does not tell', () => {
    const view: RecommendationView = {
      uuid: '0190a1b2-0000-7000-8000-00000000c001',
      displayName: 'Test Pen',
      attitude: 'avoid',
      text: 'Test text',
      dishes: ['Test Dish'],
      timeSlots: ['lunch', 'not_a_slot'],
      season: '',
      sources: [{ kind: 'chat', url: 'https://example.org/test' }],
      visitedOn: '2026-10-01',
      createdAt: '2026-11-20T10:00:00Z',
      updatedAt: '2026-11-20T10:00:00Z',
      isMine: true,
      visibility: 'private',
    }
    const values = recommendationValuesFrom(view)
    expect(toRecommendationInput(values, { signature: false })).toEqual({
      attitude: 'avoid',
      text: 'Test text',
      dishes: ['Test Dish'],
      timeSlots: ['lunch'],
      sources: [{ kind: 'chat', url: 'https://example.org/test' }],
      visitedOn: '2026-10-01',
      visibility: 'private',
    })
  })
})

describe('the place checks and request', () => {
  const place: PlaceValues = { ...emptyPlace, name: 'Test Noodles' }

  it('need a name on one line of at most 200 characters', () => {
    expect(newPlaceProblems(emptyPlace, minimal, options)).toEqual({ name: 'required' })
    expect(newPlaceProblems({ ...place, name: 'Test\nNoodles' }, minimal, options)).toEqual({ name: 'oneLine' })
    expect(newPlaceProblems({ ...place, name: wide.repeat(201) }, minimal, options)).toEqual({ name: 'tooLong' })
    expect(newPlaceProblems(place, minimal, options)).toEqual({})
  })

  it('check the landmark and the position note, and the recommendation with them', () => {
    expect(
      newPlaceProblems(
        { ...place, landmark: { type: 'text', text: ' ' }, positionNote: 'x'.repeat(501) },
        emptyRecommendation,
        options,
      ),
    ).toEqual({ landmark: 'required', positionNote: 'tooLong', attitude: 'required', text: 'required' })
    expect(newPlaceProblems({ ...place, landmark: { type: 'place', place: '' } }, minimal, options)).toEqual({
      landmark: 'required',
    })
  })

  it('sends only the name, the point and the recommendation on the shortest path', () => {
    expect(toCreatePlaceRequest(place, minimal, point)).toEqual({
      name: 'Test Noodles',
      lat: 0.34,
      lon: 7.06,
      sourceCoord: { crs: 'wgs84', source: 'crosshair' },
      recommendation: { attitude: 'good', text: 'Test one sentence', displayAs: 'main', visibility: 'circle' },
    })
  })

  it('sends the place details given, and where the point came from', () => {
    const request = toCreatePlaceRequest(
      {
        name: ' Test Noodles ',
        precision: 'approx',
        category: 'restaurant',
        cuisine: ['noodle', 'chinese'],
        positionNote: ' Test second floor ',
        landmark: { type: 'place', place: '0190a1b2-0000-7000-8000-00000000b002' },
      },
      minimal,
      { ...point, source: 'search' },
    )
    expect(request).toMatchObject({
      name: 'Test Noodles',
      precision: 'approx',
      category: 'restaurant',
      cuisine: ['chinese', 'noodle'],
      positionNote: 'Test second floor',
      landmark: { type: 'place', place: '0190a1b2-0000-7000-8000-00000000b002' },
      sourceCoord: { crs: 'wgs84', source: 'search' },
    })
    expect(
      toCreatePlaceRequest({ ...place, landmark: { type: 'text', text: ' Test gate ' } }, minimal, point).landmark,
    ).toEqual({ type: 'text', text: 'Test gate' })
  })
})

describe('the server errors on the form', () => {
  it('map the request fields to the form fields', () => {
    expect(formField('recommendation.text', 'recommendation.')).toBe('text')
    expect(formField('recommendation.sources[1].url', 'recommendation.')).toBe('sources.1.url')
    expect(formField('sources[0].kind')).toBe('sources.0.kind')
    expect(formField('recommendation.dishes[3]', 'recommendation.')).toBe('dishes')
    expect(formField('recommendation.timeSlots[0]', 'recommendation.')).toBe('timeSlots')
    expect(formField('name', 'recommendation.')).toBe('name')
    expect(formField('cuisine[2]')).toBe('cuisine')
    expect(formField('landmark.place')).toBe('landmark')
    expect(formField('landmark.text')).toBe('landmark')
    expect(formField('lat')).toBe('position')
    expect(formField('sourceCoord.source')).toBe('position')
    expect(formField('recommendation', 'recommendation.')).toBe('form')
    expect(formField('somethingNew')).toBe('form')
  })

  it('show 400s at their fields and leave the rest to the page', () => {
    const invalid = (field: string | null) => new WriteError(400, '/places', { code: 'invalid_request', field })
    expect(serverProblems(invalid('recommendation.visitedOn'), 'recommendation.')).toEqual({ visitedOn: 'invalid' })
    expect(serverProblems(invalid('text'))).toEqual({ text: 'invalid' })
    expect(serverProblems(invalid(null))).toEqual({ form: 'invalid' })
    expect(serverProblems(new WriteError(400, '/places', { code: 'outside_cities' }))).toEqual({
      position: 'outsideCities',
    })
    expect(serverProblems(new WriteError(400, '/places', { code: 'no_pseudonym' }))).toEqual({
      displayAs: 'noPseudonym',
    })
    expect(serverProblems(new WriteError(409, '/x', { code: 'already_recommended', mine: 'u' }))).toBeNull()
    expect(serverProblems(new WriteError(0, '/places'))).toBeNull()
    expect(serverProblems(new WriteError(429, '/places', { code: 'rate_limited' }))).toBeNull()
    expect(serverProblems(new Error('boom'))).toBeNull()
    expect(serverProblems(null)).toBeNull()
  })
})
