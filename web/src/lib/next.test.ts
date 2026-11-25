import { describe, expect, it } from 'vitest'
import { loginPathFor, sanitizeNext } from './next'

describe('sanitizeNext', () => {
  it.each([
    ['/', '/'],
    ['/list', '/list'],
    ['/me?tab=keys', '/me?tab=keys'],
    ['/places/0190a1b2?x=1#card', '/places/0190a1b2?x=1#card'],
    ['/a/../list', '/list'],
  ])('keeps the same-origin path %s', (raw, expected) => {
    expect(sanitizeNext(raw)).toBe(expected)
  })

  it.each([
    [null],
    [undefined],
    [''],
    ['list'],
    ['//evil.example'],
    ['//evil.example/path'],
    ['/\\evil.example'],
    ['\\\\evil.example'],
    ['/\t/evil.example'],
    ['/\n/evil.example'],
    ['https://evil.example/'],
    ['http:evil.example'],
    ['javascript:alert(1)'],
    ['data:text/html,x'],
    [' /list'],
    ['/login'],
    ['/login?next=/list'],
    ['/register'],
    ['/' + 'a'.repeat(3000)],
  ])('replaces %j with /', (raw) => {
    expect(sanitizeNext(raw)).toBe('/')
  })
})

describe('loginPathFor', () => {
  it('keeps the path and query in next', () => {
    const path = loginPathFor({ pathname: '/list', search: '?city=x&q=a b' })
    expect(path).toBe('/login?next=%2Flist%3Fcity%3Dx%26q%3Da+b')
    expect(new URLSearchParams(path.split('?')[1]).get('next')).toBe('/list?city=x&q=a b')
  })
})
