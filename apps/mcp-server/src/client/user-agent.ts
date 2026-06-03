/**
 * User-Agent sent on all Worker → backend API subrequests.
 *
 * Cloudflare Worker `fetch()` does not set a User-Agent by default, and the
 * boostedbudget.com zone has a WAF custom rule that issues a managed challenge
 * to any request with an empty User-Agent (`len(http.user_agent) == 0`). A
 * Worker cannot solve a JS challenge, so a missing UA turns every backend call
 * into a 403 "Just a moment…" interstitial. Sending a stable, identifiable UA
 * keeps that rule intact for genuinely headerless bots while letting our
 * trusted subrequests through.
 *
 * NOTE: keep this free of AI-crawler signatures (e.g. "GPTBot", "Claude") — the
 * zone has `ai_bots_protection = block`, which would block such user agents.
 */
export const MCP_WORKER_USER_AGENT = 'BoostedBudget-MCP-Worker/1.0'
