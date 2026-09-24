# ADR: AI-Crawler Policy Is Retrieval-vs-Training, Expressed Only at the Cloudflare Zone

**Date:** 2026-09-24
**Status:** Decided — **PENDING zone access** (the policy is NOT yet applied; see "Status" below)
**Issue:** #1666 (Todo 17)
**Related:** `docs/decisions/2026-09-23-agent-discovery-is-documentation-not-seo.md` (the sibling
decision that `llms.txt` is documentation, not an SEO tactic — this ADR owns the crawler policy it
defers to); `.omo/plans/1666-missing-pages-and-agentic-copy.md` (Wave 5, item 17);
`.omo/evidence/1666/zone-ai-policy.txt` (the read/apply attempt transcript);
`.omo/evidence/1666/zone-robots-consistency.txt` and `.omo/evidence/1666/zone-robots-state.txt`
(the zone↔`robots.txt` consistency reads).
**Files this decision governs:** the Cloudflare zone for `boostedbudget.com` (outside the repo);
`apps/mcp-server/src/client/user-agent.ts`; `apps/web/public/robots.txt`.

---

## Context

The repo discovered, in `apps/mcp-server/src/client/user-agent.ts`, that the
`boostedbudget.com` zone had `ai_bots_protection = block`: a note there warned that any
Worker→backend User-Agent containing an AI-crawler signature (`GPTBot`, `Claude`, …) would be
blocked. That raised the question the plan had to answer: **should BoostedBudget allow AI
crawlers to retrieve its public surface, while disallowing AI training on it?**

The approved answer (plan decision 2a) is a **retrieval/training split**: allow retrieval,
disallow training. This ADR records the decision, the mechanism that expresses it, the correct
Cloudflare lever, and — critically — the fact that it is **decided but not yet applied**.

Four facts shape the decision.

1. **`ai_bots_protection` is the wrong lever.** It is a legacy *binary* zone setting with values
   `block` / `disabled` / `only_on_ad_pages`. It does **not** distinguish retrieval from
   training. Setting it to `disabled` to "allow retrieval" would unblock **training** crawlers
   too — the exact opposite of the approved policy. Cloudflare's granular controls are separate
   fields: **`ai_search`**, **`ai_training`**, **`ai_user`**. The correct lever is
   **`ai_training = "disallow"`** (Disallow AI Training), which keeps search/retrieval and
   accountable mixed-use crawlers allowed while blocking training-only crawlers.

2. **The policy has exactly ONE home: the zone.** Cloudflare publishes a no-training preference
   into the **served** `robots.txt` via **Bot Preference Sync** when the zone is configured. If
   the committed `apps/web/public/robots.txt` also carried AI groups, the two would duplicate or
   fight. So the committed file deliberately carries **no AI-crawler groups** — the retrieval/
   training split is expressed at the zone and appears in the *served* file via Cloudflare's
   managed block. "Absent from the committed file, present at the zone" is the intended
   architecture, not a contradiction. (See `zone-robots-consistency.txt`.)

3. **UA-based rules are spoofable, so a UA probe is never proof.** Live probes on the origin
   (`apps/web/nginx.conf:130-133`, 2026-09-08) came from a **single IP rotating** `GPTBot`,
   `ClaudeBot`, `OAI-SearchBot`, and `ChatGPT-User` UAs — spoofed, since a real crawler neither
   rotates identity per request nor fetches `/.env.test`. Any rule keyed on the User-Agent
   string can be walked straight through by a client that simply sends a different one. The
   *configuration* (the Cloudflare API response showing which setting is set to which value) is
   the real evidence; a `curl` with a retrieval UA is only a **signal**. The secondary check
   added to `scripts/smoke-test.sh` is labelled exactly that way and is deliberately non-fatal.

4. **The `Google-Extended` caveat.** `Google-Extended` is a `robots.txt` user-agent token that
   controls whether Google may use content for **generative-AI model training and grounding**.
   It does **NOT** affect Google Search crawling, indexing, ranking, or AI Overviews — Google
   Search's own systems continue to crawl and may still surface the page. Blocking
   `Google-Extended` therefore does not remove the site from Google Search or from AI Overviews;
   it only opts the content out of the AI-training/grounding use. Any claim that disallowing
   `Google-Extended` removes a page from Google Search or AI Overviews is wrong, and this ADR
   rejects it. (Relevant because "disallow training" is sometimes misread as "disappear from
   Google".)

## Status — DECIDED, NOT APPLIED

This is the load-bearing part. **The policy is not live.** The zone settings could not be read,
so they could not be changed. The read/apply attempt returned:

```
GET  /zones/237fcc86cbd5635cbcacb51cfe5122a6/settings/ai_training
  {"success":false,"errors":[{"code":9109,"message":"Unauthorized to access requested resource"}],"messages":[],"result":null}   HTTP=403
GET  /zones/…/settings/ai_search            -> same 9109 / 403
GET  /zones/…/settings/ai_user              -> same 9109 / 403
GET  /zones/…/settings/is_robots_txt_managed        -> same 9109 / 403
GET  /zones/…/settings/bot_preference_sync_enabled  -> same 9109 / 403
PATCH /zones/…/settings/ai_training  {"value":"disallow"}  -> same 9109 / 403  (NOT applied)
GET  /zones/…/bot_management   -> {"code":10000,"message":"Authentication error"}   HTTP=403
```

Cause: the only available credential (a wrangler OAuth token with `zone:read`) is authorized to
read the zone record but **not** zone settings. Full transcript:
`.omo/evidence/1666/zone-ai-policy.txt`. **No value was read, none was fabricated, and no change
was applied.** The `ai_training = "disallow"` PATCH did not take effect. Re-running the probe
with a token carrying `zone_settings:read` + `zone_settings:edit` (and Bot Management read) for
zone `237fcc86cbd5635cbcacb51cfe5122a6` is the action that will move this from PENDING to
Applied.

The committed-file half of the decision *is* in place and independent of the zone: the committed
`apps/web/public/robots.txt` already carries no AI groups, and `user-agent.ts`'s comment now
describes the new policy (see Consequences).

## Decision

1. **The retrieval/training split is the policy.** Retrieval crawlers (e.g. `OAI-SearchBot`,
   `ChatGPT-User`, `PerplexityBot`) are permitted; training-only crawlers are disallowed.
2. **The lever is `ai_training = "disallow"`** — optionally accompanied by explicit `ai_search`
   / `ai_user` values — **never** `ai_bots_protection = disabled`. `ai_bots_protection` is not
   used at all for this policy.
3. **The zone is the single source of truth.** No AI-crawler groups are added to the committed
   `robots.txt`; Cloudflare owns what is published into the served file via Bot Preference Sync.
4. **A UA probe is a signal, not proof.** Configuration (the API response) is the evidence of
   record, because UAs are spoofable.
5. **`Google-Extended` does not touch Google Search or AI Overviews.** Disallowing training is
   not "de-indexing from Google".

## Consequences

- **Rollback spans two layers, and the zone half is not yet written.** Reverting this decision
  later means (i) reverting the zone setting AND (ii) reverting the documentation/comment
  changes here. Because the zone change was never applied, only (ii) exists today.
- **`user-agent.ts`.** Its comment previously recorded the old `ai_bots_protection = block`
  finding. It now describes the retrieval/training policy. The operative requirement is
  unchanged and still holds: keep the Worker→backend UA free of AI-crawler signatures, because
  `ai_training = "disallow"` has not been applied and the zone's current AI-crawler posture is
  unknown.
- **`robots.txt`.** If the AI-crawler policy ever needs to change, change it at the zone, not in
  the committed file and not in the discovery files. Adding AI groups to the committed file
  would duplicate Cloudflare's managed block and is rejected. See
  `2026-09-23-agent-discovery-is-documentation-not-seo.md`.
- **Verification is deferred.** The plan's "QA — failure" step (assert the *served* `robots.txt`
  and the zone agree) cannot run from this environment: the zone flags are unreadable (9109) and
  the apex `/robots.txt` currently 302-redirects to a Cloudflare Access login, so the served file
  cannot be fetched either. The consistency statement is therefore "cannot be asserted yet —
  PENDING", never "passed". See `zone-robots-consistency.txt`.
- **The deferred, higher-leverage lever remains #1423** (prerender the public marketing routes):
  most AI crawlers do not execute JavaScript, so retrieval quality depends on server-rendered
  HTML, not on the zone policy. #1423 is named in the sibling ADR and is not absorbed here.