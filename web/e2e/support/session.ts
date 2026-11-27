// Moves a session's last verification back in time, standing in for the ten minutes after which the server asks for
// a re-verification before sensitive changes (docs/architecture.md §3.3, auth.StepUpWindow). The test harness owns the
// instance's data directory, as an operator does: it updates the session's row in the database (SQLite in WAL mode,
// safe to write from a second process; the server reads the session on every request). The product has no setting or
// switch for this; the server is not changed or restarted.

import { createHash } from 'node:crypto'
import { join } from 'node:path'
import { DatabaseSync } from 'node:sqlite'
import type { BrowserContext } from '@playwright/test'
import { dataDir } from './env.ts'

/** The window plus a minute. */
const staleBy = 11 * 60_000

/**
 * Makes the session of the context's cookie stale: verified eleven minutes ago. instanceDir is the instance's data
 * directory: the first-start instance's by default, sharedInstance().dataDir for the shared one.
 */
export async function makeSessionStale(context: BrowserContext, instanceDir = dataDir()) {
  const cookie = (await context.cookies()).find((c) => c.name === 'osfm_session')
  if (!cookie) {
    throw new Error('e2e: no session cookie')
  }
  // sessions.token_hash is the hex SHA-256 of the cookie's token; dates are stored as PocketBase writes them.
  const hash = createHash('sha256').update(cookie.value).digest('hex')
  const verifiedAt = new Date(Date.now() - staleBy).toISOString().replace('T', ' ')
  const db = new DatabaseSync(join(instanceDir, 'pb_data', 'data.db'))
  try {
    db.exec('PRAGMA busy_timeout = 5000')
    const result = db.prepare('UPDATE sessions SET verified_at = ? WHERE token_hash = ?').run(verifiedAt, hash)
    if (result.changes !== 1) {
      throw new Error(`e2e: ${String(result.changes)} sessions updated, want 1`)
    }
  } finally {
    db.close()
  }
}
