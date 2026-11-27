// Starts the real binary for the end-to-end tests, as an operator would: `osfm serve` on an empty data directory, with
// the generated basemap archive in its tiles directory and nothing else configured. The setup code comes from the
// server log and is checked against `osfm setup-code`. The returned function stops the server and removes the data.
//
// The binary is OSFM_E2E_BINARY; without it, the setup builds one with `go build` from the web app already built in
// dist/app (pnpm run build), into node_modules/.cache/osfm-e2e. The server log goes to test-results/osfm-server.log.

import { execFileSync, spawn } from 'node:child_process'
import { once } from 'node:events'
import { createWriteStream, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'
import type { FullConfig } from '@playwright/test'
import { buildArchive } from './support/archive.ts'
import { city, portVariable, setupCodeVariable } from './support/env.ts'

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

/** The environment of the server: the caller's, without any OSFM setting that would change the instance. */
function serverEnv(dataDir: string, port: string): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {}
  for (const [k, v] of Object.entries(process.env)) {
    if (!k.startsWith('OSFM_')) {
      env[k] = v
    }
  }
  // OSFM_LISTEN rather than --http: the passkey relying party's origin takes its port from it (http://localhost:<port>).
  return { ...env, OSFM_DATA_DIR: dataDir, OSFM_LISTEN: `127.0.0.1:${port}` }
}

async function waitFor<T>(what: string, timeoutMs: number, probe: () => Promise<T | undefined>): Promise<T> {
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const value = await probe().catch(() => undefined)
    if (value !== undefined) {
      return value
    }
    if (Date.now() > deadline) {
      throw new Error(`e2e: timed out waiting for ${what}`)
    }
    await new Promise((r) => setTimeout(r, 200))
  }
}

export default async function globalSetup(config: FullConfig): Promise<() => Promise<void>> {
  const port = process.env[portVariable]
  if (!port) {
    throw new Error(`${portVariable} is not set`)
  }
  const workDir = mkdtempSync(join(tmpdir(), 'osfm-e2e-'))
  const dataDir = join(workDir, 'data')
  mkdirSync(join(dataDir, 'tiles'), { recursive: true })
  writeFileSync(join(dataDir, 'tiles', `${city.slug}.pmtiles`), buildArchive(city.archive))
  const bin = binary()

  const outputDir = config.projects[0]?.outputDir ?? join(webDir, 'test-results')
  mkdirSync(outputDir, { recursive: true })
  const logFile = join(outputDir, 'osfm-server.log')
  const log = createWriteStream(logFile)
  let output = ''
  const env = serverEnv(dataDir, port)
  const server = spawn(bin, ['serve'], { env, stdio: ['ignore', 'pipe', 'pipe'] })
  for (const stream of [server.stdout, server.stderr]) {
    stream.on('data', (chunk: Buffer) => {
      output += chunk.toString()
      log.write(chunk)
    })
  }
  const state = { exited: false }
  server.on('exit', () => {
    state.exited = true
  })
  const stop = async () => {
    if (!state.exited) {
      server.kill('SIGTERM')
      await once(server, 'exit')
    }
    log.end()
    if (!process.env.OSFM_E2E_KEEP_DATA) {
      rmSync(workDir, { recursive: true, force: true })
    } else {
      console.log(`e2e: data directory kept in ${dataDir}`)
    }
  }

  try {
    await waitFor('the health check', 30_000, async () => {
      if (state.exited) {
        return 'exited'
      }
      const r = await fetch(`http://127.0.0.1:${port}/api/health`)
      return r.ok ? 'ok' : undefined
    }).then((result) => {
      if (result === 'exited') {
        throw new Error(`e2e: the server exited; see ${logFile}`)
      }
    })
    const code = await waitFor('the setup code in the log', 10_000, () =>
      Promise.resolve(/setup_code=([0-9A-Z]{4}(?:-[0-9A-Z]{4}){3})/.exec(output)?.[1]),
    )
    const printed = execFileSync(bin, ['setup-code'], {
      env,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
    }).trim()
    if (printed !== code) {
      throw new Error(`osfm setup-code printed ${printed}, the log has ${code}`)
    }
    process.env[setupCodeVariable] = code
  } catch (e) {
    await stop()
    throw e
  }
  return stop
}
