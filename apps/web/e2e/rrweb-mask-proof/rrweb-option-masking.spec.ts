/**
 * Issue #1514 (item 4 — independent review, round 7) proved the real PostHog recorder
 * (rrweb, vended by `posthog-js/rrweb`) honors masking on a native `<option>`. Issue
 * #1683 Task 7 extends that proof to the options this app ACTUALLY ships: the spec
 * imports `sessionRecordingConfig` from `src/shared/lib/posthog.ts` and passes it
 * DIRECTLY to rrweb's `record()` through a generated JS module — `maskTextFn` is a
 * function, so the options cannot be JSON-serialized, and a hand-copied literal would
 * let this proof drift from production.
 *
 * The proof covers three claims the PII-masking audit depends on:
 *   1. `maskTextSelector: '*'` masks ALL text except the `[data-ph-unmask]` escape hatch.
 *   2. `maskAllElementAttributes: true` removes an `aria-label` ATTRIBUTE sentinel from
 *      what rrweb records (attributes are recorded verbatim and no text mask covers them).
 *   3. The same imported options object, re-imported after a navigation restarts the
 *      recorder, still masks a sensitive-route body sentinel.
 *
 * Deliberately isolated from the main E2E suite: no app server, no backend, no Clerk auth
 * — just a tiny static page and the real rrweb bundle, served from a throwaway local HTTP
 * server started in `test.beforeAll`. Run directly:
 *   cd apps/web && bun run test:rrweb-mask-proof
 * Do NOT add `.auth.spec.ts`/`.noauth.spec.ts` to this filename — that would pull it into
 * the main suite's project `testMatch` patterns and the full webServer/Clerk harness.
 */
import { test, expect } from '@playwright/test'
import { createServer, type Server } from 'http'
import { readFileSync } from 'fs'
import { createRequire } from 'module'
import { sessionRecordingConfig } from '../../src/shared/lib/posthog'

const require = createRequire(import.meta.url)

// Resolves to node_modules/posthog-js/dist/rrweb.js via posthog-js/rrweb/package.json's
// "main" field — the actual bundle posthog-js vends, not a copy or reimplementation.
const RRWEB_BUNDLE_PATH = require.resolve('posthog-js/rrweb')

const SENTINEL_PLAIN_TEXT = 'SENTINEL_PLAIN_TEXT'
const SENTINEL_UNMASK_TEXT = 'SENTINEL_UNMASK_TEXT'
const SENTINEL_ATTRIBUTE_LABEL = 'SENTINEL_ATTRIBUTE_LABEL'
const SENTINEL_SENSITIVE_ROUTE_TEXT = 'SENTINEL_SENSITIVE_ROUTE_TEXT'
const SENTINEL_NAV_UNMASK_TEXT = 'SENTINEL_NAV_UNMASK_TEXT'
const ACCOUNT_ROUTE = '/accounts/00000000-0000-4000-8000-000000000001'

const ANONYMOUS_FIXTURE_HTML = `<!doctype html>
<html>
  <body>
    <div data-ph-unmask>${SENTINEL_UNMASK_TEXT}</div>
    <div>${SENTINEL_PLAIN_TEXT}</div>
    <button aria-label="${SENTINEL_ATTRIBUTE_LABEL}">probe</button>
    <script type="module" src="/harness.js"></script>
  </body>
</html>`

const ACCOUNT_FIXTURE_HTML = `<!doctype html>
<html>
  <body>
    <div>${SENTINEL_SENSITIVE_ROUTE_TEXT}</div>
    <div data-ph-unmask>${SENTINEL_NAV_UNMASK_TEXT}</div>
    <script type="module" src="/harness.js"></script>
  </body>
</html>`

const HARNESS_JS = `
import { sessionRecordingConfig } from '/session-recording-options.js'
import { record, EventType } from '/rrweb.js'
window.__rrwebEvents = []
window.__rrwebFullSnapshotType = EventType.FullSnapshot
record({
  emit: (event) => { window.__rrwebEvents.push(event) },
  ...sessionRecordingConfig,
})
window.__rrwebReady = true
`

// Serializes the IMPORTED production object into a module the fixture page can import.
// Functions pass through `.toString()` (a real function in the page, not a JSON value);
// every other value round-trips through JSON.
function sessionRecordingOptionsModule(): string {
  const entries = Object.entries(sessionRecordingConfig).map(([key, value]) => {
    const serialized = typeof value === 'function' ? value.toString() : JSON.stringify(value)
    return `${key}: ${serialized}`
  })
  return `export const sessionRecordingConfig = {\n  ${entries.join(',\n  ')}\n}\n`
}

let server: Server
let baseUrl: string

test.beforeAll(async () => {
  const rrwebSource = readFileSync(RRWEB_BUNDLE_PATH, 'utf-8')
  const optionsModule = sessionRecordingOptionsModule()

  server = createServer((req, res) => {
    const url = req.url ?? '/'
    if (url === '/' || url === '/index.html') {
      res.writeHead(200, { 'Content-Type': 'text/html' })
      res.end(ANONYMOUS_FIXTURE_HTML)
    } else if (url.startsWith('/accounts/')) {
      res.writeHead(200, { 'Content-Type': 'text/html' })
      res.end(ACCOUNT_FIXTURE_HTML)
    } else if (url === '/harness.js') {
      res.writeHead(200, { 'Content-Type': 'application/javascript' })
      res.end(HARNESS_JS)
    } else if (url === '/session-recording-options.js') {
      res.writeHead(200, { 'Content-Type': 'application/javascript' })
      res.end(optionsModule)
    } else if (url === '/rrweb.js') {
      res.writeHead(200, { 'Content-Type': 'application/javascript' })
      res.end(rrwebSource)
    } else {
      res.writeHead(404)
      res.end()
    }
  })

  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve))
  const address = server.address()
  if (address && typeof address === 'object') {
    baseUrl = `http://127.0.0.1:${address.port}`
  } else {
    throw new Error('Failed to determine test server address')
  }
})

test.afterAll(async () => {
  await new Promise<void>((resolve, reject) => {
    server.close((err) => (err ? reject(err) : resolve()))
  })
})

async function recordedEvents(page: import('@playwright/test').Page): Promise<Array<{ type: number; data: unknown }>> {
  await page.waitForFunction(() => (window as unknown as { __rrwebReady?: boolean }).__rrwebReady === true)
  await page.waitForFunction(() => {
    const win = window as unknown as {
      __rrwebEvents?: Array<{ type: number }>
      __rrwebFullSnapshotType?: number
    }
    return win.__rrwebEvents?.some((event) => event.type === win.__rrwebFullSnapshotType) === true
  })
  return page.evaluate(() => (window as unknown as {
    __rrwebEvents: Array<{ type: number; data: unknown }>
  }).__rrwebEvents)
}

test('the imported production options mask all text except [data-ph-unmask] and mask element attributes', async ({ page }) => {
  await page.goto(baseUrl)
  const events = await recordedEvents(page)
  const serialized = JSON.stringify(events)

  expect(serialized).not.toContain(SENTINEL_PLAIN_TEXT)
  expect(serialized).toContain(SENTINEL_UNMASK_TEXT)
  expect(serialized).not.toContain(SENTINEL_ATTRIBUTE_LABEL)
  expect(serialized).not.toContain('$dead_click')
  expect(serialized).not.toContain('$autocapture')
})

test('the same imported options mask a sensitive-route sentinel after navigation restarts the recorder', async ({ page }) => {
  await page.goto(baseUrl)
  await recordedEvents(page)

  await page.goto(`${baseUrl}${ACCOUNT_ROUTE}`)
  const events = await recordedEvents(page)
  const serialized = JSON.stringify(events)

  expect(serialized).not.toContain(SENTINEL_SENSITIVE_ROUTE_TEXT)
  expect(serialized).toContain(SENTINEL_NAV_UNMASK_TEXT)
  expect(serialized).not.toContain('$dead_click')
  expect(serialized).not.toContain('$autocapture')
})
