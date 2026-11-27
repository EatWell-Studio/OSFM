// The first-start setup wizard's endpoints (G3; docs/architecture.md §3.3). While the instance has no maintainer,
// GET /api/osfm/setup says so and the app sends everyone to the wizard; once it has one, only that maintainer is still
// asked for the instance's name and cities, and afterwards the endpoint answers 404.

import { queryOptions } from '@tanstack/react-query'
import { ApiError, apiFetch, isJson } from '@/lib/api'
import { field, postAuth, unexpected } from '@/lib/authApi'
import { type Instance, parseInstance } from '@/instance/instance'
import type { Bounds } from '@/map/cities'

/** The wizard's step for the current visitor, or null when there is nothing to set up for them. */
export type SetupStep = 'maintainer' | 'instance' | null

export const setupPath = '/setup'
export const setupQueryKey = ['setup'] as const

export async function fetchSetupStep(): Promise<SetupStep> {
  const response = await apiFetch(setupPath)
  if (response.status === 404) {
    return null
  }
  if (!response.ok || !isJson(response)) {
    throw new ApiError(response.status, setupPath)
  }
  const step = field(await response.json(), 'step')
  if (step !== 'maintainer' && step !== 'instance') {
    throw unexpected(setupPath)
  }
  return step
}

export const setupQuery = queryOptions({
  queryKey: setupQueryKey,
  queryFn: fetchSetupStep,
  staleTime: Infinity,
  retry: false,
})

/** Whether the server takes code as the current setup code. */
export async function checkSetupCode(code: string): Promise<boolean> {
  const valid = field(await postAuth('/setup/check', { code }), 'valid')
  if (typeof valid !== 'boolean') {
    throw unexpected('/setup/check')
  }
  return valid
}

/** Saves the instance's name and cities, which completes the setup. */
export async function completeSetup(instance: Instance): Promise<Instance> {
  return parseInstance(await postAuth('/setup/complete', instance))
}

/**
 * The setup code as the server reads it: written like a recovery code, 16 characters of Crockford's base32 in groups
 * of four; case, spaces and dashes do not matter, and O, I and L are read as 0, 1 and 1. Null unless it has that form.
 */
export function normalizeSetupCode(input: string): string | null {
  const code = input.toUpperCase().replace(/[\s-]/g, '').replace(/O/g, '0').replace(/[IL]/g, '1')
  return /^[0-9A-HJKMNP-TV-Z]{16}$/.test(code) ? code : null
}

/** Reads "minLon,minLat,maxLon,maxLat" (commas or spaces); null unless it is a valid bounding box. */
export function parseBbox(input: string): Bounds | null {
  const parts = input
    .trim()
    .split(/[\s,]+/)
    .filter((p) => p !== '')
  if (parts.length !== 4) {
    return null
  }
  const n = parts.map(Number)
  if (!n.every((x) => Number.isFinite(x))) {
    return null
  }
  const [minLon, minLat, maxLon, maxLat] = n as Bounds
  if (minLon < -180 || maxLon > 180 || minLat < -90 || maxLat > 90 || minLon >= maxLon || minLat >= maxLat) {
    return null
  }
  return [minLon, minLat, maxLon, maxLat]
}
