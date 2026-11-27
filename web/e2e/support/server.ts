// Runs the real binary as an operator would: `osfm serve` on a data directory with the generated basemap archive in its
// tiles directory and, besides the listen address, only the trusted proxy header configured. Used by the global setup
// for each instance the tests need.

import { execFileSync, spawn } from 'node:child_process'
import { once } from 'node:events'
import { createWriteStream, mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { buildArchive } from './archive.ts'
import { city } from './env.ts'

/** The environment of the server: the caller's, without any OSFM setting that would change the instance. */
function serverEnv(dataDir: string, port: string): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = {}
  for (const [k, v] of Object.entries(process.env)) {
    if (!k.startsWith('OSFM_')) {
      env[k] = v
    }
  }
  // OSFM_LISTEN rather than --http: the passkey relying party's origin takes its port from it (http://localhost:<port>).
  // The client address from X-Forwarded-For, as behind a deployment's reverse proxy: each test phone sends its own
  // (support/browser.ts), so that the authentication rate limits count the phones separately.
  return {
    ...env,
    OSFM_DATA_DIR: dataDir,
    OSFM_LISTEN: `127.0.0.1:${port}`,
    OSFM_TRUSTED_PROXY_HEADERS: 'X-Forwarded-For',
  }
}

export async function waitFor<T>(what: string, timeoutMs: number, probe: () => Promise<T | undefined>): Promise<T> {
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

export interface Server {
  /** The setup code from the server log, checked against `osfm setup-code`. */
  setupCode: string
  stop: () => Promise<void>
}

/**
 * Starts the binary on an empty data directory (created here, with the made-up city's basemap archive) and waits for
 * its health check and its setup code. The server log goes to logFile.
 */
export async function startServer(bin: string, port: string, dataDir: string, logFile: string): Promise<Server> {
  mkdirSync(join(dataDir, 'tiles'), { recursive: true })
  writeFileSync(join(dataDir, 'tiles', `${city.slug}.pmtiles`), buildArchive(city.archive))
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
    return { setupCode: code, stop }
  } catch (e) {
    await stop()
    throw e
  }
}
