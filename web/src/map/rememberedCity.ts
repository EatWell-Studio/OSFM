// The city the map showed last on this device, so that the map and the list open on it again. A convenience only:
// without storage they open on the first city.

const rememberedCityKey = 'osfm.map.city'

/** The query parameter that names the city shown, on the map and the list. */
export const cityParam = 'city'

export function readRememberedCity(): string | null {
  try {
    return localStorage.getItem(rememberedCityKey)
  } catch {
    return null
  }
}

export function rememberCity(id: string) {
  try {
    localStorage.setItem(rememberedCityKey, id)
  } catch {
    // Remembering the city is a convenience only.
  }
}
