# ADR: llms.txt Is Agent-Readable Documentation, Not an AI-Search Tactic

**Date:** 2026-09-23
**Status:** Accepted
**Issue:** #1666
**Related:** `apps/web/src/shared/hooks/useDocumentHead.ts` (the #1423 prerendering caveat it carries);
the AI-crawler zone policy is owned by #1666 Todo 17 and recorded in its own ADR, not here.

---

## Context

Issue #1666 ships three new public pages (`/features`, `/faq`, and the previously missing `/pricing`
links from the footer) plus two discovery files: `apps/web/public/llms.txt` and the generated
`apps/web/public/llms-full.txt`. The discovery files follow the informal `llms.txt` convention: a plain
Markdown index of the site's public pages, shaped so a machine can read the product from one compact
file instead of scraping rendered pages.

Publishing a discovery file invites a question the implementation must answer explicitly, because the
answer changes what success looks like: **does this file improve how search engines or AI assistants
rank or cite BoostedBudget?** If the answer were yes, the file would carry an implied performance
obligation and its copy would need to be tuned as marketing. If the answer is no, the file is
documentation and its copy should be tuned for accuracy, not persuasion.

The researched answer is no, and the plan treats any claim to the contrary as a defect (its "Must NOT
have" list forbids "any claim that `llms.txt` improves AI-search visibility."). This ADR records that
decision and the evidence behind it, and it records the one follow-up that does move the needle.

## Decision

`llms.txt` and `llms-full.txt` are published as **agent-readable documentation**. They exist so an
agent, IDE, or MCP client can understand what BoostedBudget does and where its public pages are
without executing the single-page application.

They are **not** counted as an AI-search tactic. No ranking or citation benefit is claimed for them
in this repository, and none should be added to their copy. "Publishing `llms.txt` improves how we
appear in AI answers" is a claim this project rejects as unsupported.

The AI-crawler policy that governs which bots may retrieve versus train is a separate decision with a
separate home: it is expressed at the Cloudflare zone level (#1666 Todo 17), not in
`apps/web/public/robots.txt`, which deliberately carries no AI-crawler groups so that the policy has
exactly one source. See that task's ADR; this ADR does not restate or duplicate it.

## Why this is not an AI-search tactic

Three independent, large-sample measurements all point the same way. The figures below are the ones
#1666's research recorded; they are cited as the studies' findings, not reproduced here.

- **Ahrefs, 137,210 domains, May 2026** (server-log study of every request to `/llms.txt` across the
  population, published June 2026). 97% of valid `llms.txt` files received **zero requests** in the
  measured month. Of the small share that were fetched, 96% of requests came from bots, but AI
  *retrieval* bots (the ones that produce citations) were only **1.1%** of AI-bot requests; most fetches
  were coding agents and training crawlers. The most decisive finding is separate: requests for
  `llms.txt` files that **do not exist** came 98% from humans and **0%** from AI bots. Nothing goes
  looking for the file speculatively, so publishing one does not put a site on any AI radar. The study
  notes every figure is a ceiling, because "fetched" is not "read".
- **EZY, 83 sites over 12 weeks**. OpenAI fetched `llms.txt` 7 times versus `robots.txt` 3,990;
  Anthropic 9 versus 3,120; Perplexity **0** versus 775; Googlebot 67 versus 5,125. The one agent that
  read it at any scale was Meta-ExternalAgent (193 versus 172), which is a training/general crawler,
  not a citation engine. This is the pattern the Ahrefs log study found at population scale.
- **SE Ranking, ~300,000 domains**. About 10% of domains publish an `llms.txt`. Across statistical
  correlation and an XGBoost citation model, presence of the file showed no relationship to citation
  frequency, and **removing** `llms.txt` as a model variable *improved* predictive accuracy: the file
  added noise rather than signal. SE Ranking's conclusion is that it "doesn't seem to directly impact
  AI citation frequency."

Two further facts bound the claim from above. Google Search states its systems **ignore** `llms.txt`
and that it will "neither harm nor help" rankings. And the spec itself is an informal proposal by
Jeremy Howard / Answer.AI (v1 September 2024, v2 August 2026), not an IETF or W3C standard, so no
engine is obliged to honour it.

The one audience the studies do identify is coding and IDE agents, and MCP servers. That is exactly the
audience this repository cares about, and exactly why the file is worth publishing for comprehension
rather than for reach. A fetching agent that reads an accurate index gets a better answer; the
population of agents that never look gains nothing either way.

## The deferred finding: #1423 prerender the public marketing routes

The higher-leverage follow-up is **#1423, prerendering the public marketing routes**. It is named here
because this work cannot deliver a machine-readable *site* while the site is client-rendered, and the
reason is structural:

**Most AI crawlers do not execute JavaScript.** The per-page `<title>`, `meta[name="description"]`, and
`link[rel="canonical"]` on `/`, `/pricing`, `/features`, `/faq`, and `/mcp` are set at runtime by
`useDocumentHead` (`apps/web/src/shared/hooks/useDocumentHead.ts`). A crawler that runs the app sees
correct per-page heads, but a crawler that fetches raw HTML sees only `index.html`'s static `/`
canonical and default metadata on every route. This is stated plainly in that hook's own docstring: the
current behaviour is "best-effort SEO" and "a real fix needs prerendering (#1423)."

`llms.txt` does not close this gap. It is a separate file that a crawler has to *choose* to fetch, and
the measurements above show almost nothing does. Prerendering closes the gap for every crawler that
fetches HTML at all, whether or not it has ever heard of `llms.txt`. It is a build and CI change
(static generation of the marketing routes), which is why it is deferred out of this issue's scope
rather than absorbed into it.

## What this ADR does NOT claim

To be explicit, so a future reader cannot infer a benefit that the evidence does not support:

- It does **not** claim that `llms.txt` or `llms-full.txt` improve search rankings.
- It does **not** claim that they improve how often an AI assistant cites or recommends BoostedBudget.
- It does **not** claim that they put the site on any AI crawler's radar. The Ahrefs 404 finding shows
  the opposite: nothing fetches the file unprompted.
- It does **not** claim that they substitute for prerendering. They do not; see #1423 above.

The files' success condition is comprehension: an agent that already has a reason to read BoostedBudget's
documentation can do so cheaply and accurately. It is not, and is not presented as, discovery.

## Consequences

- **Adding a public page.** Update the route in `src/App.tsx`, the `<loc>` in `public/sitemap.xml`, and a
  link in `public/llms.txt`, then regenerate `llms-full.txt`. `src/test/seo-statics.test.ts` fails if a
  sitemap URL is missing from `llms.txt`; `marketingContent.drift.test.ts` fails if `llms-full.txt` is
  stale. Both files are documentation, so edit them for accuracy, and do not add a ranking or citation
  claim to either.
- **Copy discipline.** New copy in the discovery files stays within the `/mcp` claims discipline: no
  internal operation names, no service topology, no hand-typed capability counts (that summary is
  generated and drift-tested), no roadmap.
- **The zone is the policy home.** If the AI-crawler policy needs to change, change it at the zone
  (#1666 Todo 17), not in `robots.txt` and not in the discovery files. The committed `robots.txt`
  carrying no AI-crawler groups is the intended architecture, not an omission.
- **The next lever is #1423.** Any future effort to make the public pages genuinely machine-readable
  should go to prerendering, not to tuning the discovery files. The measurements above are why.
