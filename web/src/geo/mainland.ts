// Whether a point lies in mainland China, where the Chinese map providers use GCJ-02 (./gcj02.ts) and the navigation
// links convert to it (docs/architecture.md §4.6). Hong Kong, Macau and Taiwan are outside.
//
// The outline is deliberately rough: a few dozen vertices along the land border and out at sea, accurate to some
// kilometres in the open and to a few hundred metres only where cities meet (Shenzhen and Hong Kong, Zhuhai and Macau).
// It decides a conversion and nothing else: it is never drawn and is no statement about any border (H2).

import type { LatLon } from './gcj02'

// [lon, lat], in order around the outline, starting in the far north-east.
const outline: readonly (readonly [number, number])[] = [
  // The Amur and the Ussuri (Russia).
  [121.2, 53.4],
  [123.5, 53.65],
  [125.8, 53.2],
  [126.9, 51.6],
  [127.45, 50.32],
  [127.6, 50.2],
  [128.8, 49.4],
  [130.7, 48.9],
  [132.6, 47.8],
  [134.8, 48.4],
  [134.3, 46.7],
  [133.2, 45.6],
  [132.0, 45.3],
  [131.3, 44.6],
  [131.3, 43.4],
  [130.9, 42.9],
  [130.65, 42.4],
  // The Tumen and the Yalu (North Korea).
  [129.9, 42.9],
  [129.2, 42.3],
  [128.0, 41.95],
  [128.15, 41.45],
  [127.0, 41.75],
  [126.2, 41.1],
  [125.0, 40.55],
  [124.4, 40.1],
  [124.1, 39.8],
  // At sea: the Yellow Sea, the East China Sea and the Taiwan Strait, west of Taiwan and Penghu.
  [123.6, 38.5],
  [123.3, 36.5],
  [123.8, 31.5],
  [122.9, 28.5],
  [121.0, 26.6],
  [120.0, 25.4],
  [119.4, 24.6],
  [118.0, 23.3],
  [116.6, 22.6],
  // Around Hong Kong: Mirs Bay, the Shenzhen River and Deep Bay, the Pearl River estuary.
  [114.62, 22.45],
  [114.44, 22.55],
  [114.25, 22.565],
  [114.15, 22.535],
  [114.08, 22.53],
  [114.02, 22.51],
  [113.96, 22.5],
  [113.89, 22.46],
  [113.82, 22.4],
  [113.7, 22.25],
  // Around Macau: Gongbei to the north, Hengqin to the west.
  [113.56, 22.222],
  [113.528, 22.205],
  [113.54, 22.16],
  [113.548, 22.1],
  [113.45, 21.85],
  // The South China Sea around Hainan, the Gulf of Tonkin.
  [111.5, 21.0],
  [111.4, 18.5],
  [110.0, 17.9],
  [108.4, 18.3],
  [108.3, 21.2],
  [108.05, 21.45],
  // Vietnam, Laos, Myanmar.
  [107.95, 21.55],
  [106.8, 22.0],
  [106.65, 22.35],
  [105.4, 23.3],
  [104.4, 22.75],
  [103.95, 22.5],
  [102.5, 22.5],
  [101.85, 22.4],
  [101.7, 21.15],
  [101.15, 21.2],
  [100.2, 21.45],
  [99.25, 22.1],
  [99.5, 22.9],
  [98.9, 23.2],
  [98.7, 23.9],
  [97.8, 23.95],
  [97.55, 24.75],
  [98.6, 26.0],
  [98.7, 27.6],
  [97.5, 28.3],
  // India, Bhutan, Nepal.
  [96.0, 29.4],
  [94.0, 29.1],
  [92.0, 27.8],
  [90.3, 28.25],
  [89.15, 27.75],
  [88.95, 27.3],
  [88.75, 27.55],
  [88.1, 27.9],
  [86.0, 27.9],
  [84.0, 28.6],
  [81.0, 30.1],
  [79.4, 31.0],
  [78.7, 31.9],
  [79.2, 32.6],
  [78.1, 35.5],
  // Pakistan, Afghanistan, Tajikistan, Kyrgyzstan, Kazakhstan.
  [76.5, 35.9],
  [75.4, 36.85],
  [74.9, 37.2],
  [74.9, 38.5],
  [73.5, 39.45],
  [73.95, 39.75],
  [75.4, 40.5],
  [76.5, 40.4],
  [77.8, 41.0],
  [79.0, 41.8],
  [80.2, 42.2],
  [80.2, 42.9],
  [80.4, 44.2],
  [80.8, 45.0],
  [82.55, 45.2],
  [82.3, 45.6],
  [82.4, 46.8],
  [83.0, 47.2],
  [85.5, 47.05],
  [86.8, 48.6],
  [87.35, 49.17],
  // Mongolia.
  [88.0, 48.6],
  [90.0, 47.9],
  [90.9, 46.6],
  [91.0, 45.3],
  [95.4, 44.3],
  [96.4, 42.75],
  [100.8, 42.65],
  [105.0, 41.6],
  [107.0, 42.4],
  [109.8, 42.6],
  [111.9, 43.7],
  [113.5, 44.8],
  [116.0, 44.9],
  [116.6, 46.3],
  [117.5, 46.6],
  [119.9, 46.7],
  [119.7, 47.6],
  [117.7, 48.0],
  [115.6, 47.9],
  [116.7, 49.85],
  // The Argun (Russia), back to the Amur.
  [117.9, 49.6],
  [119.1, 50.3],
  [119.8, 51.3],
  [120.8, 52.6],
]

/** Whether the WGS-84 position lies inside the rough outline of mainland China (ray casting). */
export function inMainlandChina({ lat, lon }: LatLon): boolean {
  let inside = false
  for (let i = 0, j = outline.length - 1; i < outline.length; j = i++) {
    const [xi, yi] = outline[i] ?? [0, 0]
    const [xj, yj] = outline[j] ?? [0, 0]
    if (yi > lat !== yj > lat && lon < ((xj - xi) * (lat - yi)) / (yj - yi) + xi) {
      inside = !inside
    }
  }
  return inside
}
