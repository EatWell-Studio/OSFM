import { CircleSettings } from './CircleSettings'
import { InstanceSettings } from './InstanceSettings'

/** The instance settings, /admin/settings: name and cities, then the report threshold, invites and place search. */
export function SettingsAdmin() {
  return (
    <>
      <InstanceSettings />
      <CircleSettings />
    </>
  )
}
