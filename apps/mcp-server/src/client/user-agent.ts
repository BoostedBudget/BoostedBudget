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
 * NOTE: keep this free of AI-crawler signatures (e.g. "GPTBot", "Claude"). The zone's
 * AI-crawler policy is to ALLOW retrieval and DISALLOW training, expressed only at the
 * Cloudflare zone via `ai_training = "disallow"` (#1666 Todo 17) — deliberately NOT via
 * the legacy `ai_bots_protection`, whose `disabled` value would unblock training crawlers
 * too. ⚠ That zone change is PENDING, not applied: the API read/write returned Cloudflare
 * error 9109 "Unauthorized to access requested resource", so the zone's current posture is
 * unknown and a signature-bearing UA must still be avoided. See
 * docs/decisions/2026-09-24-ai-crawler-policy-retrieval-vs-training.md. UA rules are also
 * spoofable, so this is hygiene, not a security boundary.
 */
export const MCP_WORKER_USER_AGENT = 'BoostedBudget-MCP-Worker/1.0'
