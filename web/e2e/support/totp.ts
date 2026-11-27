// An authenticator app for the tests: the TOTP codes of a secret (SHA-1, 30 s, 6 digits, as the server issues them).
// The server takes each time step only once per member (docs/architecture.md §3.3), so a code for a step that has been
// used already waits for the next step.

import * as OTPAuth from 'otpauth'

const period = 30

function currentStep(): number {
  return Math.floor(Date.now() / 1000 / period)
}

export class AuthenticatorApp {
  private readonly totp: OTPAuth.TOTP
  private lastStep = -1

  /** secret is the base32 key as the registration page shows it (spaces between groups are ignored). */
  constructor(secret: string) {
    this.totp = new OTPAuth.TOTP({
      secret: OTPAuth.Secret.fromBase32(secret.replace(/\s/g, '')),
      algorithm: 'SHA1',
      digits: 6,
      period,
    })
  }

  /** The code of the current step, after waiting for a step later than the last one used. */
  async code(): Promise<string> {
    while (currentStep() <= this.lastStep) {
      const untilNext = (this.lastStep + 1) * period * 1000 - Date.now()
      await new Promise((r) => setTimeout(r, Math.max(untilNext, 0) + 250))
    }
    this.lastStep = currentStep()
    return this.totp.generate({ timestamp: this.lastStep * period * 1000 })
  }
}
