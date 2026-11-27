// Starts the real binary for the end-to-end tests, twice, as an operator would (support/server.ts):
//
// - the first-start instance: an empty data directory whose setup wizard circle.e2e.ts goes through in the browser
//   (baseURL(), setupCode() and dataDir() in support/env.ts);
// - the shared instance: set up here through the API, with a maintainer and the made-up city, for the specs of the
//   features (support/shared.ts: sharedInstance(), inviteMember(), createPlace()). Each spec file adds its own members
//   and data to it; none relies on what another spec file left.
//
// The returned function stops both servers and removes the data.
//
// The binary is OSFM_E2E_BINARY; without it, the setup builds one with `go build` from the web app already built in
// dist/app (pnpm run build), into node_modules/.cache/osfm-e2e. The server logs go to test-results/osfm-server.log and
// test-results/osfm-server-shared.log.

import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import type { FullConfig } from '@playwright/test'
import {
  dataDirVariable,
  portVariable,
  setupCodeVariable,
  sharedPortVariable,
  sharedStateVariable,
} from './support/env.ts'
import { startServer } from './support/server.ts'
import { setUpSharedInstance } from './support/shared.ts'

const webDir = resolve(import.meta.dirname, '..')
const repoDir = resolve(webDir, '..')

function binary(): string {
  const given = process.env.OSFM_E2E_BINARY
  if (given) {
    return resolve(given)
  }
  if (!existsSync(join(webDir, 'dist', 'app', 'index.html'))) {
    throw new Error('No built web app in web/dist/app: run `pnpm run build` first, or set OSFM_E2E_BINARY.')
  }
  // Not under the system's temporary directory: PocketBase takes a binary there for `go run` and turns on its
  // development mode (every SQL statement on stdout).
  const out = join(webDir, 'node_modules', '.cache', 'osfm-e2e', 'osfm')
  console.log('e2e: building the binary (go build ./cmd/osfm)')
  execFileSync('go', ['build', '-o', out, './cmd/osfm'], { cwd: repoDir, stdio: 'inherit' })
  return out
}

function variable(name: string): string {
  const value = process.env[name]
  if (!value) {
    throw new Error(`${name} is not set`)
  }
  return value
}

export default async function globalSetup(config: FullConfig): Promise<() => Promise<void>> {
  const port = variable(portVariable)
  const sharedPort = variable(sharedPortVariable)
  const workDir = mkdtempSync(join(tmpdir(), 'osfm-e2e-'))
  const bin = binary()
  const outputDir = config.projects[0]?.outputDir ?? join(webDir, 'test-results')
  mkdirSync(outputDir, { recursive: true })

  const stops: (() => Promise<void>)[] = []
  const stop = async () => {
    for (const s of stops.reverse()) {
      await s()
    }
    if (!process.env.OSFM_E2E_KEEP_DATA) {
      rmSync(workDir, { recursive: true, force: true })
    } else {
      console.log(`e2e: data directories kept in ${workDir}`)
    }
  }

  try {
    const dataDir = join(workDir, 'data')
    const fresh = await startServer(bin, port, dataDir, join(outputDir, 'osfm-server.log'))
    stops.push(fresh.stop)
    process.env[setupCodeVariable] = fresh.setupCode
    process.env[dataDirVariable] = dataDir

    const sharedDir = join(workDir, 'shared')
    const shared = await startServer(bin, sharedPort, sharedDir, join(outputDir, 'osfm-server-shared.log'))
    stops.push(shared.stop)
    const state = await setUpSharedInstance(sharedPort, sharedDir, shared.setupCode)
    const stateFile = join(workDir, 'shared-instance.json')
    writeFileSync(stateFile, JSON.stringify(state), { mode: 0o600 })
    process.env[sharedStateVariable] = stateFile
  } catch (e) {
    await stop()
    throw e
  }
  return stop
}
