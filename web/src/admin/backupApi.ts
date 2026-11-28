// Backups of the instance (G5; internal/api/backups.go): maintainers list, create, download and delete them. Creating,
// downloading and deleting need a fresh proof (reauth_required otherwise). Restoring is done on the server with
// `osfm restore`, never from the web app.

import {
  BackupDeletePath,
  type BackupInfo,
  BackupKindOSFM,
  BackupKindPremigrate,
  BackupPath,
  BackupsPath,
} from '@/api/types.gen'
import { apiFetch, isJson, retryAfterSeconds } from '@/lib/api'
import { AuthApiError, field, postAuth, unexpected } from '@/lib/authApi'

export type { BackupInfo }

export const backupsQueryKey = ['backups'] as const

const relative = (path: string) => path.replace(/^\/api\/osfm/, '')

/** The API path of a backup route for the file name (names are [a-z0-9_.], encoded all the same). */
function named(pattern: string, name: string): string {
  return relative(pattern.replace('{name}', encodeURIComponent(name)))
}

export function isBackupInfo(v: unknown): v is BackupInfo {
  const kind = field(v, 'kind')
  return (
    typeof field(v, 'name') === 'string' &&
    (kind === BackupKindOSFM || kind === BackupKindPremigrate) &&
    typeof field(v, 'size') === 'number' &&
    typeof field(v, 'createdAt') === 'string'
  )
}

/** Throws an AuthApiError for an unsuccessful response, with the server's code when it sent one. */
async function refuse(response: Response, path: string): Promise<never> {
  let code: string | null = null
  if (isJson(response)) {
    const c = field(await response.json().catch(() => null), 'code')
    code = typeof c === 'string' ? c : null
  }
  throw new AuthApiError(response.status, code, path, retryAfterSeconds(response))
}

/** GET /api/osfm/backups: newest first. */
export async function fetchBackups(): Promise<BackupInfo[]> {
  const path = relative(BackupsPath)
  const response = await apiFetch(path)
  if (!response.ok) {
    return refuse(response, path)
  }
  const list = field(await response.json(), 'backups')
  if (!Array.isArray(list) || !list.every(isBackupInfo)) {
    throw unexpected(path)
  }
  return list
}

/** POST /api/osfm/backups: writes a backup now and returns it. */
export async function createBackup(): Promise<BackupInfo> {
  const path = relative(BackupsPath)
  const created = await postAuth(path, {})
  if (!isBackupInfo(created)) {
    throw unexpected(path)
  }
  return created
}

/** GET /api/osfm/backups/{name}: the file. */
export async function downloadBackup(name: string): Promise<Blob> {
  const path = named(BackupPath, name)
  const response = await apiFetch(path, { headers: { Accept: 'application/zip' } })
  if (!response.ok) {
    return refuse(response, path)
  }
  return response.blob()
}

/** POST /api/osfm/backups/{name}/delete. */
export async function deleteBackup(name: string): Promise<void> {
  const path = named(BackupDeletePath, name)
  const response = await apiFetch(path, { method: 'POST' })
  if (!response.ok) {
    return refuse(response, path)
  }
}

/** A file size in the page's language: kilobytes below one megabyte, megabytes above. */
export function formatSize(bytes: number, language: string): string {
  const mb = bytes / 1_000_000
  const [value, unit] = mb < 1 ? [bytes / 1000, 'kilobyte'] : [mb, 'megabyte']
  return new Intl.NumberFormat(language, { style: 'unit', unit, maximumFractionDigits: 1 }).format(value)
}
