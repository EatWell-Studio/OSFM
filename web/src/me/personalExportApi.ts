// The member's personal data export (E6): GET /api/osfm/me/export (internal/api/personal_export.go), a JSON file the
// browser saves.

import { PersonalExportFilePrefix, PersonalExportPath } from '@/api/types.gen'
import { apiFetch, retryAfterSeconds } from '@/lib/api'

/** The path of the export relative to the API base (apiFetch takes paths under /api/osfm). */
const path = PersonalExportPath.replace(/^\/api\/osfm/, '')

/** The file name the server gives in Content-Disposition, or a name of the same form when there is none. */
export function exportFileName(disposition: string | null, now = new Date()): string {
  const match = /filename="([\w.-]+)"/.exec(disposition ?? '')
  return match?.[1] ?? `${PersonalExportFilePrefix}${now.toISOString().slice(0, 10)}.json`
}

/** A refused or failed download: the status, and for a 429 the seconds to wait. */
export class PersonalExportError extends Error {
  readonly status: number
  readonly retryAfter: number | null

  constructor(status: number, retryAfter: number | null) {
    super(`${PersonalExportPath}: HTTP ${String(status)}`)
    this.name = 'PersonalExportError'
    this.status = status
    this.retryAfter = retryAfter
  }
}

/**
 * Downloads the member's personal export (GET /api/osfm/me/export) and saves it as a file. A blob: URL, not a link to
 * the route: the download needs the session cookie and the app's error handling, and the content security policy
 * allows no data: URLs.
 */
export async function downloadPersonalExport(): Promise<void> {
  const response = await apiFetch(path)
  if (!response.ok) {
    throw new PersonalExportError(response.status, retryAfterSeconds(response))
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(new Blob([blob], { type: 'application/json' }))
  const a = document.createElement('a')
  a.href = url
  a.download = exportFileName(response.headers.get('Content-Disposition'))
  document.body.append(a)
  a.click()
  a.remove()
  setTimeout(() => {
    URL.revokeObjectURL(url)
  }, 0)
}
