// What entitles someone to create an account: an invite (D1), or, for the first maintainer only, the setup code that
// the server prints when it starts without a maintainer (G3; docs/architecture.md §3.3), which comes with the instance
// name the server saves together with the maintainer. The registration components take one and post to the matching
// endpoints; the server finds these values in the request body under the same names as here.

export type Entitlement = { invite: string } | { setupCode: string; instanceName: string }

export interface RegistrationPaths {
  passwordStart: string
  passwordConfirm: string
  passkeyOptions: string
  passkey: string
}

const invitePaths: RegistrationPaths = {
  passwordStart: '/register/password/start',
  passwordConfirm: '/register/password/confirm',
  passkeyOptions: '/passkey/register/options',
  passkey: '/passkey/register',
}

const setupPaths: RegistrationPaths = {
  passwordStart: '/setup/password/start',
  passwordConfirm: '/setup/password/confirm',
  passkeyOptions: '/setup/passkey/options',
  passkey: '/setup/passkey',
}

/** The endpoints (relative to /api/osfm) of a registration with this entitlement. */
export function registrationPaths(entitlement: Entitlement): RegistrationPaths {
  return 'setupCode' in entitlement ? setupPaths : invitePaths
}
