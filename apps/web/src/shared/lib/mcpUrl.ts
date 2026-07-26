/**
 * Resolves the BoostedBudget MCP server URL.
 *
 * Issue #1417 (tracked separately, not fixed here): `apps/web/public/config.js` hardcodes
 * `ENVIRONMENT: 'development'`, and production deploys Cloudflare Pages from `apps/web/dist`
 * — `docker-entrypoint.sh` (which would rewrite `config.js` at container start) is never
 * invoked in that path. So `window.ENV?.ENVIRONMENT` is the string `'development'` in
 * production today. The common codebase idiom `window.ENV?.X || import.meta.env.VITE_X`
 * (see sentry.ts, posthog.ts) is therefore wrong for anything environment-derived — it
 * short-circuits on that stale runtime value.
 *
 * This helper avoids that bug: build-time `import.meta.env.VITE_MCP_URL` is checked FIRST
 * and wins over `window.ENV`, not the other way around. It also fails closed — returns
 * `null` rather than guessing — whenever it cannot determine a real MCP Worker URL.
 */

/** Known MCP Worker URLs, keyed by `VITE_ENVIRONMENT`. No entry for 'qa': no MCP Worker
 * is deployed to QA (see scripts/smoke-test.sh), so 'qa' must resolve to null, not staging. */
const MCP_URL_BY_ENVIRONMENT: Readonly<Record<string, string>> = {
  production: 'https://mcp.boostedbudget.com',
  staging: 'https://staging-mcp.boostedbudget.com',
}

/**
 * Validates a candidate MCP URL: trims whitespace, requires it to parse as an absolute
 * URL with protocol exactly `https:`, and strips any trailing slash. Returns `null` for
 * anything that fails validation so the caller can fall through to the next source.
 */
function normalizeCandidate(candidate: string | null | undefined): string | null {
  if (!candidate) return null

  const trimmed = candidate.trim()
  if (!trimmed) return null

  let parsed: URL
  try {
    parsed = new URL(trimmed)
  } catch {
    return null
  }

  if (parsed.protocol !== 'https:') return null

  return trimmed.replace(/\/+$/, '')
}

/**
 * Resolves the MCP server base URL. Resolution order (first valid match wins):
 *
 *   1. `import.meta.env.VITE_MCP_URL` — build-time, authoritative.
 *   2. `window.ENV?.MCP_URL` — runtime override. This exists for Docker / self-hosted
 *      deployments only, where `docker-entrypoint.sh` regenerates `public/config.js` at
 *      container start. The Cloudflare Pages production/staging path ships a static
 *      `config.js` (see module doc above), so this branch never activates there.
 *   3. `import.meta.env.VITE_ENVIRONMENT` mapped to a known MCP Worker URL.
 *   4. `null` — fail closed. Covers `'qa'`, `'development'`, an unrecognised string, an
 *      empty string, and absent.
 *
 * A malformed value from step 1 or 2 falls through to the next step rather than
 * short-circuiting to `null`.
 */
export function getMcpBaseUrl(): string | null {
  const buildTime = normalizeCandidate(import.meta.env.VITE_MCP_URL)
  if (buildTime) return buildTime

  const runtime = normalizeCandidate(window.ENV?.MCP_URL)
  if (runtime) return runtime

  const environment = import.meta.env.VITE_ENVIRONMENT
  if (environment && environment in MCP_URL_BY_ENVIRONMENT) {
    return MCP_URL_BY_ENVIRONMENT[environment]
  }

  return null
}

/** Same resolution as {@link getMcpBaseUrl}, with `/mcp` appended. `null` when the base is `null`. */
export function getMcpEndpoint(): string | null {
  const base = getMcpBaseUrl()
  if (!base) return null

  return `${base}/mcp`
}
