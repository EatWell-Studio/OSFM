import i18n from 'i18next'
import { zhCN } from '@/i18n/resources'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { PlaceCard, RecommendationView } from './placeApi'
import { excerptLength, shareOrCopy, shareText } from './share'

// Made-up data only.
const origin = 'https://food.example.org'
const placeId = '0190a1b2-0000-7000-8000-00000000b001'
const recId = '0190a1b2-0000-7000-8000-00000000c001'

function rec(overrides: Partial<RecommendationView> = {}): RecommendationView {
  return {
    uuid: recId,
    displayName: 'Test Member',
    attitude: 'must_try',
    text: 'Test text',
    dishes: [],
    timeSlots: [],
    season: '',
    sources: [],
    createdAt: '2026-11-20T10:00:00Z',
    updatedAt: '2026-11-20T10:00:00Z',
    isMine: false,
    ...overrides,
  }
}

function card(overrides: Partial<PlaceCard> = {}): PlaceCard {
  return {
    uuid: placeId,
    name: 'Test Noodle House',
    aliases: [],
    lat: 0.34,
    lon: 7.06,
    precision: 'exact',
    positionNote: 'Test second floor\nbehind the   market',
    category: '',
    cuisine: [],
    city: 'example-bay',
    counts: { mustTry: 2, good: 1, ok: 0, avoid: 0 },
    recommendations: [rec()],
    ...overrides,
  }
}

const zh = i18n.getFixedT<['share', 'place', 'common']>('zh-CN', ['share', 'place', 'common'])
const en = i18n.getFixedT<['share', 'place', 'common']>('en', ['share', 'place', 'common'])

describe('shareText', () => {
  it('describes the place on one line, then gives its link (M2-Q15)', () => {
    const { attitude } = zhCN.common
    const withCity = zhCN.share.text.placeWithCity
      .replace('{{name}}', 'Test Noodle House')
      .replace('{{city}}', 'Example Bay')
    expect(shareText({ place: card(), cityName: 'Example Bay', origin, t: zh })).toBe(
      `${withCity} · Test second floor behind the market · ${attitude.must_try} 2 · ${attitude.good} 1\n${origin}/p/${placeId}`,
    )
    expect(shareText({ place: card(), cityName: 'Example Bay', origin, t: en })).toBe(
      `Test Noodle House (Example Bay) · Test second floor behind the market · Must try 2 · Good 1\n${origin}/p/${placeId}`,
    )
  })

  it('leaves out what the place does not have', () => {
    const place = card({ positionNote: '', counts: { mustTry: 0, good: 0, ok: 0, avoid: 0 } })
    expect(shareText({ place, cityName: '', origin, t: en })).toBe(`Test Noodle House\n${origin}/p/${placeId}`)
  })

  it('adds a recommendation: attitude, signature and the start of its text, and links to it', () => {
    const recommendation = rec({ text: 'Test <b>hand-pulled</b>\nnoodles' })
    expect(shareText({ place: card(), recommendation, cityName: 'Example Bay', origin, t: en })).toBe(
      [
        'Test Noodle House (Example Bay) · Test second floor behind the market · Must try 2 · Good 1',
        'Must try · Test Member: Test <b>hand-pulled</b> noodles',
        `${origin}/p/${placeId}?r=${recId}`,
      ].join('\n'),
    )
  })

  it(`quotes only the first ${String(excerptLength)} characters of a recommendation, whole characters`, () => {
    // A wide character, then an emoji of two code points with a skin tone: one character each.
    const wide = '\u9762'
    const long = wide.repeat(excerptLength - 1) + '👍🏽' + 'Test tail'
    const text = shareText({ place: card(), recommendation: rec({ text: long }), cityName: '', origin, t: zh })
    const line = text.split('\n')[1] ?? ''
    expect(line.endsWith(wide.repeat(excerptLength - 1) + '👍🏽…')).toBe(true)
    expect(text).not.toContain('Test tail')

    const exact = 'x'.repeat(excerptLength)
    const whole = shareText({ place: card(), recommendation: rec({ text: exact }), cityName: '', origin, t: en })
    expect(whole.split('\n')[1]).toBe(`Must try · Test Member: ${exact}`)
  })

  it('names a member who left as such', () => {
    const recommendation = rec({ displayName: '', authorLeft: true })
    expect(shareText({ place: card(), recommendation, cityName: '', origin, t: en }).split('\n')[1]).toBe(
      'Must try · A member who left: Test text',
    )
  })
})

describe('shareOrCopy', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function stubNavigator(share?: (data: ShareData) => Promise<void>, writeText?: (text: string) => Promise<void>) {
    vi.stubGlobal('navigator', {
      ...(share ? { share: vi.fn(share) } : {}),
      clipboard: { writeText: vi.fn(writeText ?? (() => Promise.resolve())) },
    })
    return navigator as unknown as {
      share?: ReturnType<typeof vi.fn>
      clipboard: { writeText: ReturnType<typeof vi.fn> }
    }
  }

  it('uses the share sheet when there is one, with the link inside the text', async () => {
    const nav = stubNavigator(() => Promise.resolve())
    expect(await shareOrCopy('Test text\nhttps://food.example.org/p/x')).toBe('shared')
    expect(nav.share).toHaveBeenCalledWith({ text: 'Test text\nhttps://food.example.org/p/x' })
    expect(nav.clipboard.writeText).not.toHaveBeenCalled()
  })

  it('does nothing more when the member closes the share sheet', async () => {
    const nav = stubNavigator(() => Promise.reject(new DOMException('closed', 'AbortError')))
    expect(await shareOrCopy('Test text')).toBe('cancelled')
    expect(nav.clipboard.writeText).not.toHaveBeenCalled()
  })

  it('copies when the browser refuses the share sheet', async () => {
    const nav = stubNavigator(() => Promise.reject(new DOMException('no', 'NotAllowedError')))
    expect(await shareOrCopy('Test text')).toBe('copied')
    expect(nav.clipboard.writeText).toHaveBeenCalledWith('Test text')
  })

  it('copies when there is no share sheet', async () => {
    const nav = stubNavigator()
    expect(await shareOrCopy('Test text')).toBe('copied')
    expect(nav.clipboard.writeText).toHaveBeenCalledWith('Test text')
  })

  it('fails when neither works', async () => {
    stubNavigator(undefined, () => Promise.reject(new DOMException('no', 'NotAllowedError')))
    expect(await shareOrCopy('Test text')).toBe('failed')
  })
})
