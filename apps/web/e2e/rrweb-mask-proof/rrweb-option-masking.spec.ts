/**
 * Issue #1514 (item 4 — independent review, round 7): our own scanner tests only ever
 * ask `scanForUnmaskedPII` whether a masked ANCESTOR exists in the DOM (see
 * pii-masking.ts's `hasMaskedAncestor`) — that proves our own test infrastructure agrees
 * with itself, not that PostHog's actual session-replay recorder (rrweb) honors
 * `data-ph-mask` on a native `<option>` element specifically. `<option>` has an unusual
 * rendering model (its content isn't laid out like normal text — some serializers special-
 * case it), so this was a real, previously-unverified assumption underpinning eight of
 * this branch's `<option>`-text fixes (see the #1514 fix-round reports).
 *
 * This test runs the EXACT module PostHog's session-recording extension uses internally —
 * `posthog-js/rrweb`, which resolves to `dist/rrweb.js` inside the installed `posthog-js`
 * package (see its `rrweb/package.json` subpath: `"main": "../dist/rrweb.js"`). It is not
 * a separate rrweb dependency and not a reimplementation — it is the real recorder,
 * invoked with the same `maskTextSelector`/`maskAllInputs` options this app's
 * `posthog.ts` passes to `posthog.init()`.
 *
 * Deliberately isolated from the main E2E suite: no app server, no backend, no Clerk auth
 * — just a tiny static page (a masked and an unmasked `<option>` inside a `<select>`) and
 * the real rrweb bundle, served from a throwaway local HTTP server started in
 * `test.beforeAll`. Run directly:
 *   cd apps/web && npx playwright test --config=e2e/rrweb-mask-proof/playwright.config.ts
 * Do NOT add `.auth.spec.ts`/`.noauth.spec.ts` to this filename — that would pull it into
 * the main suite's project `testMatch` patterns and the full webServer/Clerk harness.
 */
import { test, expect } from '@playwright/test'
import { createServer, type Server } from 'http'
import { readFileSync } from 'fs'
import { createRequire } from 'module'

const require = createRequire(import.meta.url)

// Resolves to node_modules/posthog-js/dist/rrweb.js via posthog-js/rrweb/package.json's
// "main" field — the actual bundle posthog-js vends, not a copy or reimplementation.
const RRWEB_BUNDLE_PATH = require.resolve('posthog-js/rrweb')

const SENTINEL_MASKED = 'SENTINEL_MASKED_OPTION_TEXT'
const SENTINEL_UNMASKED = 'SENTINEL_UNMASKED_OPTION_TEXT'

const FIXTURE_HTML = `<!doctype html>
<html>
  <body>
    <select id="the-select">
      <option value="masked" data-ph-mask>${SENTINEL_MASKED}</option>
      <option value="unmasked">${SENTINEL_UNMASKED}</option>
    </select>
    <script type="module" src="/harness.js"></script>
  </body>
</html>`

const HARNESS_JS = `
import { record, EventType } from '/rrweb.js'
window.__rrwebEvents = []
window.__rrwebFullSnapshotType = EventType.FullSnapshot
record({
  emit: (event) => { window.__rrwebEvents.push(event) },
  maskTextSelector: '[data-ph-mask]',
  maskAllInputs: true,
})
window.__rrwebReady = true
`

let server: Server
let baseUrl: string

test.beforeAll(async () => {
  const rrwebSource = readFileSync(RRWEB_BUNDLE_PATH, 'utf-8')

  server = createServer((req, res) => {
    if (req.url === '/' || req.url === '/index.html') {
      res.writeHead(200, { 'Content-Type': 'text/html' })
      res.end(FIXTURE_HTML)
    } else if (req.url === '/harness.js') {
      res.writeHead(200, { 'Content-Type': 'application/javascript' })
      res.end(HARNESS_JS)
    } else if (req.url === '/rrweb.js') {
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

test('rrweb (the real PostHog recorder) masks a data-ph-mask <option>\'s text and leaves an unmasked <option> readable', async ({ page }) => {
  await page.goto(baseUrl)
  await page.waitForFunction(() => (window as unknown as { __rrwebReady?: boolean }).__rrwebReady === true)

  const result = await page.evaluate(() => {
    const win = window as unknown as {
      __rrwebEvents: Array<{ type: number; data: unknown }>
      __rrwebFullSnapshotType: number
    }

    const fullSnapshot = win.__rrwebEvents.find((e) => e.type === win.__rrwebFullSnapshotType)
    if (!fullSnapshot) return { found: false as const, serialized: '' }

    // Serialize the whole snapshot node tree to a string and search it textually —
    // simplest way to answer "does this substring appear ANYWHERE in what rrweb
    // recorded," without needing to know rrweb's exact serialized-node shape in this
    // version. This is deliberately the same coarse-but-decisive check as the rest of
    // this PII-masking suite's sentinel substring matching.
    const serialized = JSON.stringify(fullSnapshot)
    return { found: true as const, serialized }
  })

  expect(result.found).toBe(true)
  expect(result.serialized).not.toContain('SENTINEL_MASKED_OPTION_TEXT')
  expect(result.serialized).toContain('SENTINEL_UNMASKED_OPTION_TEXT')
})
