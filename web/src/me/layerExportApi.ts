// The per-layer GeoJSON exports (F1, M2-Q17): GET /api/osfm/exports/places and /exports/recommendations with an
// optional ?city= (internal/api/layer_export.go). The file is fetched with the session cookie and handed to the
// browser as a download; the format is in docs/data-format.md.

import { ExportParamCity, ExportPlacesPath, ExportRecommendationsPath } from '@/api/types.gen'
import { apiFetch, isJson, retryAfterSeconds } from '@/lib/api'
import { field } from '@/lib/authApi'

export type Layer = 'places' | 'recommendations'

const paths: Record<Layer, string> = {
  places: ExportPlacesPath.replace(/^\/api\/osfm/, ''),
  recommendations: ExportRecommendationsPath.replace(/^\/api\/osfm/, ''),
}

/** The path of a layer's export relative to the API base, limited to the city with this slug unless it is empty. */
export function layerExportPath(layer: Layer, city: string): string {
  const path = paths[layer]
  return city === '' ? path : `${path}?${new URLSearchParams({ [ExportParamCity]: city }).toString()}`
}

/** A refused export: the HTTP status, the server's code and, for a 429, the seconds to wait. */
export class LayerExportError extends Error {
  readonly status: number
  readonly code: string | null
  readonly retryAfter: number | null

  constructor(status: number, code: string | null, retryAfter: number | null) {
    super(`layer export: HTTP ${String(status)}${code ? ` (${code})` : ''}`)
    this.name = 'LayerExportError'
    this.status = status
    this.code = code
    this.retryAfter = retryAfter
  }
}

/** The file name the server gave in Content-Disposition when it is a plain one, otherwise a default. */
export function exportFileName(response: Response, layer: Layer): string {
  const match = /filename="([a-z0-9][a-z0-9.-]*\.geojson)"/.exec(response.headers.get('Content-Disposition') ?? '')
  return match?.[1] ?? `osfm-${layer}.geojson`
}

/** Fetches the export; throws LayerExportError when it is refused. */
export async function fetchLayerExport(layer: Layer, city: string): Promise<{ blob: Blob; fileName: string }> {
  const response = await apiFetch(layerExportPath(layer, city), { headers: { Accept: 'application/geo+json' } })
  if (!response.ok) {
    let code: string | null = null
    if (isJson(response)) {
      const c = field(await response.json().catch(() => null), 'code')
      code = typeof c === 'string' ? c : null
    }
    throw new LayerExportError(response.status, code, retryAfterSeconds(response))
  }
  return { blob: await response.blob(), fileName: exportFileName(response, layer) }
}

/** Hands blob to the browser as a download named fileName. */
export function saveFile(blob: Blob, fileName: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = fileName
  link.rel = 'noopener'
  link.hidden = true
  document.body.append(link)
  link.click()
  link.remove()
  // Revoked after the click has been handled: some browsers start the download asynchronously.
  setTimeout(() => {
    URL.revokeObjectURL(url)
  }, 0)
}
