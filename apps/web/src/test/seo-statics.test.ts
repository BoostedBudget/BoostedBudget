/**
 * SEO statics tests
 *
 * Verifies the SEO-facing static files (robots.txt, sitemap.xml) and the
 * index.html metadata corrections landed by issue #1411. These are plain
 * filesystem assertions -- no XML parser dependency is introduced (see
 * apps/web/src/features/landing/LandingPage.css.test.ts for the pattern
 * this follows).
 *
 * Paths are resolved relative to this file (and therefore the repo), not
 * the process cwd, so the test works from any invocation directory.
 */

import { describe, it, expect } from 'bun:test'
import { readFileSync, existsSync, writeFileSync, rmSync } from 'fs'
import { dirname, join } from 'path'
import { tmpdir } from 'os'
import { fileURLToPath } from 'url'
import { POSTHOG_ENGAGEMENT_ROUTES } from '@/shared/posthog/routes'

const currentDir = dirname(fileURLToPath(import.meta.url))
const webRoot = join(currentDir, '..', '..') // apps/web

const robotsPath = join(webRoot, 'public', 'robots.txt')
const sitemapPath = join(webRoot, 'public', 'sitemap.xml')
const indexHtmlPath = join(webRoot, 'index.html')
const ogImagePath = join(webRoot, 'public', 'og-image.png')
const appTsxPath = join(webRoot, 'src', 'App.tsx')

/**
 * #1666 Todo 12 — robots.txt ↔ App.tsx sync guard.
 *
 * robots.txt's own header claims its
 * authenticated Disallow list is "derived from the routes wrapped in
 * `<ProtectedRoute>` in apps/web/src/App.tsx -- not guessed". Nothing enforced
 * that discipline: a new `<ProtectedRoute>` route could ship with no matching
 * `Disallow` and no test would notice. These helpers close that gap.
 *
 * The parsing is deliberately pure-string (App.tsx is read as TEXT, never
 * imported) and takes its inputs as parameters, so the meta-test below can feed
 * it a scratch fixture without touching the real route table.
 */

// Reads a JSX opening tag starting at `start` (the `<` of `<Route`), respecting
// brace nesting and string literals so a `>` inside an attribute value (e.g. an
// arrow function in an `element={...}`) does not terminate the tag early.
function readRouteOpeningTag(source: string, start: number): string {
  let depth = 0
  let quote: string | null = null
  for (let i = start; i < source.length; i++) {
    const ch = source[i]
    if (quote !== null) {
      if (ch === '\\') {
        i++
        continue
      }
      if (ch === quote) quote = null
      continue
    }
    if (ch === '"' || ch === "'" || ch === '`') {
      quote = ch
      continue
    }
    if (ch === '{') {
      depth++
      continue
    }
    if (ch === '}') {
      depth--
      continue
    }
    if (ch === '>' && depth === 0) {
      return source.slice(start, i + 1)
    }
  }
  return source.slice(start)
}

// Every `<Route>` whose element wraps `<ProtectedRoute>`, resolved to a concrete
// path. `path={POSTHOG_ENGAGEMENT_ROUTES.<key>}` is resolved through the imported
// constant object (per the header's "not guessed" claim); a literal `path="<value>"`
// is taken verbatim (that is how `/admin` is declared).
function resolveProtectedPaths(
  appSource: string,
  routes: Readonly<Record<string, string>>
): string[] {
  const paths: string[] = []
  const routeOpen = /<Route(?=[\s/>])/g
  let match: RegExpExecArray | null
  while ((match = routeOpen.exec(appSource)) !== null) {
    const tag = readRouteOpeningTag(appSource, match.index)
    if (!/<ProtectedRoute(?=[\s/>])/.test(tag)) continue

    const constantRef = /path=\{POSTHOG_ENGAGEMENT_ROUTES\.([A-Za-z0-9_]+)\}/.exec(tag)
    if (constantRef) {
      const key = constantRef[1]
      // An unresolved key must surface rather than vanish: it is reported as an
      // uncovered route below, because no Disallow can match this token.
      paths.push(routes[key] ?? `POSTHOG_ENGAGEMENT_ROUTES.${key}`)
      continue
    }

    const literal = /path="([^"]+)"/.exec(tag)
    if (literal) paths.push(literal[1])
  }
  return [...new Set(paths)]
}

function parseDisallowPaths(robots: string): string[] {
  return Array.from(robots.matchAll(/^Disallow:\s*(\S+)\s*$/gm)).map((m) => m[1])
}

// Mirrors robots.txt's own matching semantics: a Disallow covers a route when the
// route equals it or begins with it (`/accounts` covers `/accounts/reconnect`).
function isCovered(disallowed: string, routePath: string): boolean {
  return routePath === disallowed || routePath.startsWith(disallowed)
}

function findUncoveredProtectedRoutes(
  appSource: string,
  robots: string,
  routes: Readonly<Record<string, string>>
): string[] {
  const disallows = parseDisallowPaths(robots)
  return resolveProtectedPaths(appSource, routes).filter(
    (routePath) => !disallows.some((disallowed) => isCovered(disallowed, routePath))
  )
}

describe('robots.txt', () => {
  it('exists and is non-empty', () => {
    expect(existsSync(robotsPath)).toBe(true)
    const content = readFileSync(robotsPath, 'utf-8')
    expect(content.trim().length).toBeGreaterThan(0)
  })

  it('contains a Sitemap directive pointing at the production sitemap', () => {
    const content = readFileSync(robotsPath, 'utf-8')
    expect(content).toMatch(/^Sitemap:\s*https:\/\/boostedbudget\.com\/sitemap\.xml\s*$/m)
  })

  it('disallows authenticated application areas (spot-check)', () => {
    const content = readFileSync(robotsPath, 'utf-8')
    expect(content).toMatch(/^Disallow:\s*\/dashboard\s*$/m)
    expect(content).toMatch(/^Disallow:\s*\/admin\s*$/m)
  })

  // #1411 N2 fix: these three routes (/cancel-deletion/:token, /households/invites/:token,
  // /household/invite/:token in apps/web/src/App.tsx) are unprotected (no <ProtectedRoute>
  // wrapper) but carry a one-time token in the path. A crawlable, indexable tokenized URL
  // (an invite link, a deletion-cancellation link) is exactly the kind of leak robots.txt
  // exists to prevent, even though the routes aren't auth-gated. (Deliberately not citing
  // line numbers here -- they drift every time App.tsx is edited.)
  it('disallows unprotected routes that carry a one-time token in the path', () => {
    const content = readFileSync(robotsPath, 'utf-8')
    expect(content).toMatch(/^Disallow:\s*\/cancel-deletion\s*$/m)
    expect(content).toMatch(/^Disallow:\s*\/households\/\s*$/m)
    expect(content).toMatch(/^Disallow:\s*\/household\/\s*$/m)
  })
})

// #1666 Todo 12: robots.txt header claims its Disallow list is derived from the
// `<ProtectedRoute>` routes in App.tsx. This makes that claim testable instead of
// aspirational. The happy path is expected to pass on the current tree; the value
// is the meta-test, which proves the guard actually bites on a new route.
describe('robots.txt <-> App.tsx protected-route sync (#1666)', () => {
  const appSource = readFileSync(appTsxPath, 'utf-8')
  const robotsSource = readFileSync(robotsPath, 'utf-8')

  it('disallows every route wrapped in <ProtectedRoute>', () => {
    const protectedPaths = resolveProtectedPaths(appSource, POSTHOG_ENGAGEMENT_ROUTES)

    // A floor so a broken extractor that collects nothing cannot pass vacuously.
    expect(protectedPaths.length).toBeGreaterThanOrEqual(15)

    const uncovered = findUncoveredProtectedRoutes(
      appSource,
      robotsSource,
      POSTHOG_ENGAGEMENT_ROUTES
    )

    // Reported on success so the resolved set is auditable, not just asserted.
    console.log(
      `[robots-sync] resolved protected-path set (${protectedPaths.length}): ` +
        protectedPaths.join(', ')
    )

    expect(
      uncovered,
      `robots.txt does not Disallow protected route(s): ${uncovered.join(', ')}`
    ).toEqual([])
  })

  it('fails on a route the Disallow list does not cover (meta-test)', () => {
    const scratchRoute =
      '<Route path="/scratch-not-covered" element={<ProtectedRoute><ScratchPage /></ProtectedRoute>} />'
    const scratchFile = join(tmpdir(), `robots-sync-fixture-${Date.now()}.tsx`)

    try {
      // Copy App.tsx to a scratch fixture plus ONE extra protected route. The real
      // App.tsx is never modified.
      writeFileSync(scratchFile, `${appSource}\n${scratchRoute}\n`, 'utf-8')
      const fixtureSource = readFileSync(scratchFile, 'utf-8')

      const uncovered = findUncoveredProtectedRoutes(
        fixtureSource,
        robotsSource,
        POSTHOG_ENGAGEMENT_ROUTES
      )
      console.log(`[robots-sync] fixture uncovered routes: ${uncovered.join(', ')}`)

      expect(
        uncovered,
        `robots.txt does not Disallow protected route(s): ${uncovered.join(', ')}`
      ).toEqual(['/scratch-not-covered'])
    } finally {
      rmSync(scratchFile, { force: true })
    }
  })
})

describe('sitemap.xml', () => {
  const content = readFileSync(sitemapPath, 'utf-8')
  const robotsContent = readFileSync(robotsPath, 'utf-8')
  const disallowedPaths = Array.from(robotsContent.matchAll(/^Disallow:\s*(\S+)\s*$/gm)).map(
    (m) => m[1]
  )

  it('exists and parses as XML with a urlset root', () => {
    expect(existsSync(sitemapPath)).toBe(true)
    expect(content).toMatch(/<urlset[^>]*>/)
    expect(content).toMatch(/<\/urlset>\s*$/)
  })

  const publicUrls = [
    'https://boostedbudget.com/',
    'https://boostedbudget.com/pricing',
    'https://boostedbudget.com/mcp',
    // #1666: the two public pages created by this plan. They are routable for
    // signed-out and signed-in users alike (App.tsx declares them beside
    // /pricing), so they belong in the sitemap exactly like the other public
    // routes.
    'https://boostedbudget.com/features',
    'https://boostedbudget.com/faq',
    'https://boostedbudget.com/legal/privacy-policy',
    'https://boostedbudget.com/legal/terms',
    'https://boostedbudget.com/legal/cookie-policy',
  ]

  it.each(publicUrls)('contains a <loc> for %s', (url) => {
    expect(content).toContain(`<loc>${url}</loc>`)
  })

  it('contains no lastmod, changefreq, or priority elements', () => {
    expect(content).not.toMatch(/<lastmod>/)
    expect(content).not.toMatch(/<changefreq>/)
    expect(content).not.toMatch(/<priority>/)
  })

  it('lists at least one Disallow entry to check the sitemap against (guards against an empty/broken robots.txt parse silently no-op-ing the loop below)', () => {
    expect(disallowedPaths.length).toBeGreaterThan(0)
  })

  // Path-prefix match (mirroring robots.txt's own Disallow semantics), not a raw substring
  // check against the whole <loc> string: a substring check would false-fail on a
  // hypothetical future legitimate URL that merely *contains* a disallowed segment later in
  // its path (e.g. a blog post at /blog/about-households/history would contain the string
  // "/households/" without actually being under the disallowed /households/ path).
  const sitemapPaths = Array.from(content.matchAll(/<loc>(.*?)<\/loc>/g)).map(
    (m) => new URL(m[1]).pathname
  )

  it.each(disallowedPaths)(
    'contains no <loc> whose path is disallowed by robots.txt: %s',
    (disallowedPath) => {
      const prefix = disallowedPath.endsWith('/') ? disallowedPath : `${disallowedPath}/`
      const violating = sitemapPaths.filter(
        (path) => path === disallowedPath || path.startsWith(prefix)
      )
      expect(violating, `sitemap contains disallowed path(s): ${violating.join(', ')}`).toHaveLength(0)
    }
  )
})

describe('index.html metadata corrections (#1411)', () => {
  const content = readFileSync(indexHtmlPath, 'utf-8')

  it('does not contain the stale "2-month" trial wording', () => {
    expect(content).not.toContain('2-month')
  })

  it('does not contain the stale priceValidUntil property', () => {
    expect(content).not.toContain('priceValidUntil')
  })

  it('does not overclaim "intelligent automation rules"', () => {
    expect(content).not.toContain('intelligent automation rules')
  })

  it('describes automation rules as user-defined', () => {
    expect(content).toContain('user-defined automation rules')
  })
})

// #1666: the social unfurl image. Absent before this change, which meant every
// link shared to Slack/X/Facebook unfurled with no image. A `public/` asset is
// copied verbatim, not bundled, so there is no bundle-budget impact.
describe('social unfurl image (#1666)', () => {
  const content = readFileSync(indexHtmlPath, 'utf-8')
  const ogImageUrl = 'https://boostedbudget.com/og-image.png'

  it('ships a non-empty PNG at public/og-image.png', () => {
    expect(existsSync(ogImagePath)).toBe(true)
    const bytes = readFileSync(ogImagePath)
    expect(bytes.length).toBeGreaterThan(0)
    // PNG magic number: 0x89 "PNG" -- not an SVG, which social platforms do not render.
    expect(bytes.subarray(0, 8).toString('latin1')).toBe('\x89PNG\r\n\x1a\n')
  })

  it('declares og:image with the absolute production URL', () => {
    expect(content).toContain(`<meta property="og:image" content="${ogImageUrl}" />`)
  })

  it('declares the 1200x630 Open Graph image dimensions', () => {
    expect(content).toContain('<meta property="og:image:width" content="1200" />')
    expect(content).toContain('<meta property="og:image:height" content="630" />')
  })

  it('declares twitter:image with the absolute production URL', () => {
    expect(content).toContain(`<meta name="twitter:image" content="${ogImageUrl}" />`)
  })
})
