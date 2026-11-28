// WGS-84 → GCJ-02, the obfuscated datum that maps of mainland China use (docs/architecture.md §4.6). The database holds
// WGS-84 only; this conversion is needed for nothing but the links to navigation apps that expect GCJ-02 there.
//
// The formula is the widely published one (Krasovsky ellipsoid, semi-major axis 6 378 245 m): every open
// implementation (eviltransform, coordtransform, gcoord) uses it and agrees with the conversion of the Chinese map
// providers to well under a metre. It applies everywhere; whether a point needs it is ./mainland.ts's decision. There is
// no inverse here: OSFM never reads GCJ-02 (parsing shared links of Chinese apps is P1).

/** A position in degrees. */
export interface LatLon {
  lat: number
  lon: number
}

const a = 6378245.0
const ee = 0.006693421622965943

function offsetLat(x: number, y: number): number {
  let d = -100.0 + 2.0 * x + 3.0 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x))
  d += ((20.0 * Math.sin(6.0 * x * Math.PI) + 20.0 * Math.sin(2.0 * x * Math.PI)) * 2.0) / 3.0
  d += ((20.0 * Math.sin(y * Math.PI) + 40.0 * Math.sin((y / 3.0) * Math.PI)) * 2.0) / 3.0
  d += ((160.0 * Math.sin((y / 12.0) * Math.PI) + 320.0 * Math.sin((y * Math.PI) / 30.0)) * 2.0) / 3.0
  return d
}

function offsetLon(x: number, y: number): number {
  let d = 300.0 + x + 2.0 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x))
  d += ((20.0 * Math.sin(6.0 * x * Math.PI) + 20.0 * Math.sin(2.0 * x * Math.PI)) * 2.0) / 3.0
  d += ((20.0 * Math.sin(x * Math.PI) + 40.0 * Math.sin((x / 3.0) * Math.PI)) * 2.0) / 3.0
  d += ((150.0 * Math.sin((x / 12.0) * Math.PI) + 300.0 * Math.sin((x / 30.0) * Math.PI)) * 2.0) / 3.0
  return d
}

/** The GCJ-02 position of a WGS-84 one (an offset of a few hundred metres in China; applied wherever the point is). */
export function wgs84ToGcj02({ lat, lon }: LatLon): LatLon {
  const x = lon - 105.0
  const y = lat - 35.0
  const radLat = (lat / 180.0) * Math.PI
  const magic = 1 - ee * Math.sin(radLat) * Math.sin(radLat)
  const sqrtMagic = Math.sqrt(magic)
  const dLat = (offsetLat(x, y) * 180.0) / (((a * (1 - ee)) / (magic * sqrtMagic)) * Math.PI)
  const dLon = (offsetLon(x, y) * 180.0) / ((a / sqrtMagic) * Math.cos(radLat) * Math.PI)
  return { lat: lat + dLat, lon: lon + dLon }
}
