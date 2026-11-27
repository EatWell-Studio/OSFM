// The instance settings besides the name and the cities (maintainers): the report threshold (D11), the invite
// cooling-off period (D12) and place search (B7). They are fields of GET/POST /api/osfm/instance, which a maintainer
// reads with all fields; the server checks them (reportThreshold ≥ 1, inviteCooldownDays ≥ 0) and writes the
// settings.update audit entry. These are the only calls of the form (CircleSettings.tsx), so that the wiring to the
// server stays in this file.

import { queryOptions } from '@tanstack/react-query'
import { type Instance, instancePath, parseInstance } from '@/instance/instance'
import { apiJson } from '@/lib/api'
import { field, postAuth } from '@/lib/authApi'

export interface CircleSettings {
  /** Open reports that hide a recommendation until a maintainer reviews it; at least 1. */
  reportThreshold: number
  /** Days after joining before a new member can invite; 0 for none. */
  inviteCooldownDays: number
  /** Whether members can search for place names (through this server). */
  geocoderEnabled: boolean
  /** The contact sent to the search services with each request; may be empty. */
  geocoderContact: string
}

/** The longest contact the form accepts. */
export const maxGeocoderContact = 200

export const circleSettingsQueryKey = ['instance', 'circle-settings'] as const

function isCount(value: unknown, min: number): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= min
}

/**
 * Reads the settings from a body of GET or POST /api/osfm/instance; null when the server does not send them (a server
 * without these settings yet).
 */
export function parseCircleSettings(body: unknown): CircleSettings | null {
  const reportThreshold = field(body, 'reportThreshold')
  const inviteCooldownDays = field(body, 'inviteCooldownDays')
  const geocoderEnabled = field(body, 'geocoderEnabled')
  const geocoderContact = field(body, 'geocoderContact')
  if (!isCount(reportThreshold, 1) || !isCount(inviteCooldownDays, 0) || typeof geocoderEnabled !== 'boolean') {
    return null
  }
  return {
    reportThreshold,
    inviteCooldownDays,
    geocoderEnabled,
    geocoderContact: typeof geocoderContact === 'string' ? geocoderContact : '',
  }
}

export const circleSettingsQuery = queryOptions({
  queryKey: circleSettingsQueryKey,
  queryFn: async () => parseCircleSettings(await apiJson(instancePath)),
  staleTime: 0,
})

/**
 * POST /api/osfm/instance with these settings, together with the name and cities as saved now (read again just
 * before, so that the request does not undo a change made meanwhile). Returns what the server saved, and the instance.
 */
export async function saveCircleSettings(
  settings: CircleSettings,
): Promise<{ settings: CircleSettings; instance: Instance }> {
  const current = parseInstance(await apiJson(instancePath))
  const body = await postAuth(instancePath, { name: current.name, cities: current.cities, ...settings })
  return { settings: parseCircleSettings(body) ?? settings, instance: parseInstance(body) }
}
