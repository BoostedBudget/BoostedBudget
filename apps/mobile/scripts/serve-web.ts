#!/usr/bin/env bun
/**
 * Static file server for the exported mobile web build (`dist/web`).
 *
 * This exists so the Playwright visual/accessibility layer can drive the REAL static export
 * without `expo start`. It is deliberately zero-dependency: the repository's `serve`
 * devDependency is root-only and is not part of the `apps/mobile` workspace graph, and the plan
 * forbids adding `serve`/`http-server`/`sirv`. `Bun.serve` plus `node:fs`/`node:path` is enough.
 *
 * Routing: Expo Router's `web.output: "static"` export writes one HTML file per route
 * (`index.html`, `gallery.html`, …). A request for `/gallery` resolves to `gallery.html`; a
 * request for a real asset (`/_expo/static/…`) resolves to that file. Missing paths fall back to
 * the exported `+not-found.html` so a typo surfaces as the app's own 404 rather than a blank page.
 *
 * Usage:
 *   bun run --cwd apps/mobile serve:web            # http://127.0.0.1:8081, serves dist/web
 *   E2E_VISUAL_PORT=9099 bun run serve:web
 *   E2E_WEB_ROOT=dist/web-clerk-smoke bun run serve:web
 *
 * `E2E_WEB_ROOT` overrides which export directory is served, resolved against this package's root
 * (an absolute path is used as-is). It defaults to `dist/web`, the inert export the visual suite
 * drives. The credentialed Clerk smoke points it at its OWN keyed export (`dist/web-clerk-smoke`):
 * `EXPO_PUBLIC_*` is inlined at build time, so serving the inert export to that suite would
 * false-skip, and building the keyed export into `dist/web` would clobber the artifact the gating
 * visual step depends on.
 *
 * The root must already exist; run `bun run --cwd apps/mobile export:web` (or the smoke's own
 * export script) first. Failing loudly here (instead of serving an empty directory) keeps the
 * Playwright layer from reporting a confusing "no elements found" failure when the export is
 * simply missing.
 */
import { existsSync, statSync } from 'node:fs'
import { dirname, join, normalize, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

/**
 * The subset of Bun's runtime globals this server uses. Declared locally rather than adding
 * `@types/bun`: the plan forbids any dependency beyond the two Playwright packages, and this
 * package's tsconfig restricts the auto-included `types` to jest + react-native-css.
 */
declare const Bun: {
  serve(options: {
    port: number
    hostname: string
    fetch: (request: Request) => Response | Promise<Response>
  }): { port: number }
  file(path: string): Blob
}

const SCRIPT_DIR = dirname(fileURLToPath(import.meta.url))
// `E2E_WEB_ROOT` is relative to this package's root (`dist/web` by default); an absolute value is
// used unchanged because `resolve` ignores the earlier segments when a later one is absolute.
const ROOT = resolve(SCRIPT_DIR, '..', process.env.E2E_WEB_ROOT ?? 'dist/web')
const PORT = Number(process.env.E2E_VISUAL_PORT ?? '8081')
const HOSTNAME = process.env.E2E_VISUAL_HOST ?? '127.0.0.1'

if (!existsSync(ROOT)) {
  console.error(
    `serve-web: no static export at ${ROOT}. Run \`bun run --cwd apps/mobile export:web\` first.`,
  )
  process.exit(1)
}

/** True when `candidate` is the root itself or lives underneath it (blocks `..` traversal). */
function isInsideRoot(candidate: string): boolean {
  return candidate === ROOT || candidate.startsWith(ROOT + sep)
}

/**
 * Candidate files for a URL path, most specific first. An extension-less path also tries
 * `<path>.html` (Expo's per-route file) and `<path>/index.html` (directory index).
 */
function candidatePaths(pathname: string): string[] {
  let decoded = pathname
  try {
    decoded = decodeURIComponent(pathname)
  } catch {
    // A malformed percent-escape is not a path we can serve; fall through with the raw value.
  }
  const relative = normalize(decoded).replace(/^[/\\]+/, '')
  const base = resolve(ROOT, relative)
  const candidates = [base]
  if (!/\.[A-Za-z0-9]+$/.test(relative)) {
    candidates.push(`${base}.html`, join(base, 'index.html'))
  }
  return candidates
}

function findFile(pathname: string): string | null {
  for (const candidate of candidatePaths(pathname)) {
    if (!isInsideRoot(candidate)) continue
    try {
      if (statSync(candidate).isFile()) return candidate
    } catch {
      // Not a regular file; try the next candidate.
    }
  }
  return null
}

const server = Bun.serve({
  port: PORT,
  hostname: HOSTNAME,
  fetch(request) {
    const url = new URL(request.url)
    const file = findFile(url.pathname)
    if (file) {
      // `Bun.file` derives the Content-Type from the extension.
      return new Response(Bun.file(file))
    }
    const notFound = findFile('/+not-found')
    if (notFound) {
      return new Response(Bun.file(notFound), { status: 404 })
    }
    return new Response('Not found', { status: 404, headers: { 'content-type': 'text/plain' } })
  },
})

console.log(`serve-web: serving ${ROOT} at http://${HOSTNAME}:${server.port}`)
