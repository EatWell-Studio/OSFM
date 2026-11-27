// End-to-end tests (docs/architecture.md §6): the real binary from an empty data directory, driven in Chromium with a
// mobile viewport. Run with `pnpm run e2e`; see CONTRIBUTING.md.

import { existsSync } from 'node:fs'
import { createServer } from 'node:net'
import { chromium, defineConfig, devices } from '@playwright/test'
import { portVariable, sharedPortVariable } from './e2e/support/env.ts'

/** A free TCP port on 127.0.0.1. */
function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createServer()
    server.on('error', reject)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      server.close(() => {
        if (address && typeof address === 'object') {
          resolve(address.port)
        } else {
          reject(new Error('no port'))
        }
      })
    })
  })
}

// Chosen once, in the main process; the workers load this file again and inherit the variables. One port for the
// first-start instance, one for the shared instance of the feature specs (e2e/global-setup.ts).
process.env[portVariable] ??= String(await freePort())
process.env[sharedPortVariable] ??= String(await freePort())

/**
 * The browser: OSFM_E2E_CHANNEL names a Playwright channel ("chrome" for the installed Google Chrome), or "bundled" for
 * Playwright's own Chromium. Unset, Playwright's Chromium is used when it is installed (pnpm exec playwright install
 * chromium), the system's Google Chrome otherwise.
 */
function channel(): string | undefined {
  const choice = process.env.OSFM_E2E_CHANNEL
  if (choice) {
    return choice === 'bundled' ? undefined : choice
  }
  return existsSync(chromium.executablePath()) ? undefined : 'chrome'
}

export default defineConfig({
  testDir: './e2e',
  testMatch: '*.e2e.ts',
  globalSetup: './e2e/global-setup.ts',
  // The flows of a spec file build on each other; the spec files share the instances and run in order, in one worker.
  workers: 1,
  fullyParallel: false,
  retries: 0,
  forbidOnly: !!process.env.CI,
  // A sign-in with TOTP may wait for the next 30-second step.
  timeout: 90_000,
  expect: { timeout: 15_000 },
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : [['list']],
  outputDir: 'test-results',
  projects: [
    {
      name: 'chromium-mobile',
      use: { ...devices['Pixel 7'], channel: channel() },
    },
  ],
})
