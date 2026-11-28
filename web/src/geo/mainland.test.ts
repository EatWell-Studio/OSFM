import { describe, expect, it } from 'vitest'
import { inMainlandChina } from './mainland'

// Approximate positions of well-known places, made up to a few hundred metres.
const inside: [string, number, number][] = [
  ['central China', 30.59, 114.3],
  ['the south-west', 30.66, 104.07],
  ['the north-west', 43.82, 87.62],
  ['the north-east', 45.8, 126.53],
  ['Hainan', 20.04, 110.32],
  ['Tibet', 29.65, 91.12],
  ['the far west', 39.47, 75.99],
  ['an island off the east coast', 30.0, 122.2],
  ['Shenzhen, by the border with Hong Kong', 22.54, 114.06],
  ['Shekou', 22.48, 113.91],
  ['Zhuhai, by the border with Macau', 22.23, 113.55],
  ['Hengqin', 22.12, 113.52],
  ['Xiamen', 24.48, 118.09],
  ['a border town on the Amur', 50.24, 127.49],
  ['a border town by Vietnam', 21.55, 107.97],
  ['the Chumbi valley', 27.48, 88.91],
]

const outside: [string, number, number][] = [
  ['Hong Kong Island', 22.28, 114.16],
  ['Kowloon', 22.32, 114.17],
  ['the New Territories by the border', 22.5, 114.13],
  ['Macau peninsula', 22.1936, 113.5397],
  ['Taipa', 22.157, 113.556],
  ['Taipei', 25.03, 121.56],
  ['Penghu', 23.57, 119.58],
  ['Seoul', 37.57, 126.98],
  ['Pyongyang', 39.03, 125.75],
  ['Hanoi', 21.03, 105.85],
  ['Ulaanbaatar', 47.92, 106.92],
  ['Almaty', 43.25, 76.9],
  ['Vladivostok', 43.12, 131.89],
  ['a Russian town on the Amur', 50.27, 127.53],
  ['Thimphu', 27.47, 89.64],
  ['Kathmandu', 27.7, 85.32],
  ['Tokyo', 35.68, 139.69],
  ['northern Germany', 52.37452, 9.73861],
]

describe('inMainlandChina', () => {
  it.each(inside)('is true in %s', (_, lat, lon) => {
    expect(inMainlandChina({ lat, lon })).toBe(true)
  })

  it.each(outside)('is false in %s', (_, lat, lon) => {
    expect(inMainlandChina({ lat, lon })).toBe(false)
  })
})
