# apps/web

React 19 / TypeScript 5 / Vite 8 frontend for BoostedBudget.

## Development

```bash
bun run dev         # Start dev server
bun run build       # Production build
bun run test        # Unit tests (bun:test)
bun run lint        # ESLint
bun run typecheck   # TypeScript type-check
bun run test:e2e    # Playwright E2E tests
```

## Bundle-size budgets

Per-route chunk budgets are enforced by [`size-limit`](https://github.com/ai/size-limit) and configured in `.size-limit.json` at the repo root. The `bundle-size` CI job (`.github/workflows/test.yml`) fails any PR that:
- Has a chunk exceeding its per-route budget, **or**
- Emits a new Rolldown chunk with no matching budget entry ("orphan chunk")

**Budget policy:**
- Per-feature chunks are budgeted at their gzipped baseline + 20%.
- The shared `vendor` chunk sits at **~84.8% of its 460 kB limit** (390.18 kB measured, 69.82 kB headroom — see the stamped table below). It fell from 449.32 kB in #1650 when the consent-gated `posthog-js` SDK moved out of `vendor` into its own lazily-loaded `posthog` chunk; before that it was budget-saturated at ~97.7%. Re-read the current figures off the stamped table rather than this sentence — the paragraph records the shape of the budget, not a live number. Raising the limit requires a measured before/after in the PR description — and measure on a clean `bun install`, because a stale `node_modules` can carry duplicate copies of a package and inflate this figure by double digits.
- The limit was last raised **410 → 435 kB in #1513**, the consolidated Dependabot bump. Attribution was measured by bisecting in a clean worktree off `staging`: baseline 408.32 kB, final 428.36 kB, and essentially all of the +20 kB came from the eight-package `@radix-ui/*` group (PR #1317) — gridstack 12→13 contributed ~0.7 kB. The growth is spread across the group rather than concentrated: applied individually, `react-dialog` costs +6.1 kB, the other six +8.0 kB and `react-avatar` +0.9 kB, but applied together they cost +19.3 kB, because their shared internals dedupe worse across eight simultaneously-bumped packages. There was no single package to hold back. **If `vendor` needs to come back down, the lever is splitting `@radix-ui/*` into its own lazy chunk** via a `codeSplitting.groups` entry plus a matching `.size-limit.json` budget — deliberately not done in #1513 to keep that PR dependency-only.
- `admin` was raised **20 → 24 kB in #1631**. Measured before/after in the same clean run: 18.97 kB → 20.26 kB, +1.29 kB gzipped, for the structured DLQ error view (`JobErrorList` + `parseJobErrors`, which replaced a one-line JSON dump with the Postgres context an operator needs). The old 20 kB limit had only 1.02 kB of headroom, so the feature cleared it by 262 B; the new limit is the measured size + 20% per the policy above rather than a nudge past the failure, which would have left the next admin change to fail for this one's reasons.
- **#1650 moved the consent-gated `posthog-js` SDK off the eager path and raised `shared` 40 → 57 kB.** The 1.386.8 → 1.430.2 bump plus the new analytics lifecycle broke three gates at the branch HEAD (`vendor` 478.11/460, `shared` 47.4/40, landing route 571.17 KiB/542). The SDK is consent-gated and nothing on `/` needs it, so it is now loaded via a dynamic import and given its own lazy `posthog` chunk — remeasured in one run: `vendor` 384.21 kB (was 478.11), landing route **476 KiB** (was 571.17). `shared` keeps 44.84 kB against its raised 57 kB limit; the +20% policy on the pre-split 47.4 kB measurement gives 57. The `posthog` chunk measures 95.8 kB against 117 kB (measured + 20% rounded). The `vendor` and route budgets were deliberately NOT raised: the fix was removing eager weight, not widening the gates. ⚠ The chunk split needs BOTH halves in `vite.config.ts` — the `node_modules` group AND the `src/shared/posthog/` source group. Groups assign chunks by PATH, so with only the first half the `shared` chunk keeps a static edge to the SDK and the SDK is still modulepreloaded on `/` (verified on the first build of this branch).
- #1652 isolates `@revenuecat/purchases-js` in the lazy `revenuecat` chunk. The existing vendor catch-all absorbed the SDK despite its dynamic import, measuring 670.29 kB. A higher-priority group reduces vendor to 449.01 kB; the SDK measures 219.715 kB and has a 264 kB budget (baseline + 20%, rounded). The vendor limit stays 460 kB. A production smoke test requires the separate SDK chunk and verifies `/` does not request it.
- #1652 also raises `shared` **40 → 49 kB** and `admin` **24 → 30 kB**, using measured baselines + 20%, rounded. Shared measures 40.327 kB (previous stamped baseline 38.59 kB), including the checkout identity/cancellation controller and application-wide confirmation hook. Admin measures 24.948 kB (previous 20.28 kB), including RevenueCat diagnostics, nullable authority/health rendering, grant controls and operation history. The landing route measures **535 KiB against its unchanged 542 KiB limit**; SDK bytes remain outside its initial download.
- #1637/#1627 adds a detail-only `budget-plan` chunk: 3.76 kB gzip, budgeted at 4.6 kB (measured size + 20%, rounded). A higher-priority group preserves the panel's lazy boundary; the feature-wide rule had absorbed it into `budgets`, measuring 26.24 kB against 25 kB. After splitting, `budgets` measures 23.33 kB and keeps its existing limit. No dependencies were added. (The landing route now measures 476 KiB against its 542 KiB limit after #1650's SDK lazy-load; it was 533 KiB when this bullet was written.)
- Initial budgets were set at the time lazy-loading was introduced (issue #1126); updated to Rolldown baselines in issue #1225.

**If CI fails on bundle size:**
1. **Chunk exceeded its budget** — either shrink the chunk back below budget, or raise the budget in the same PR with a reasoned justification in the PR description (e.g., "added echarts component, +X KB gzipped").
2. **Orphan chunk detected** — a new Rolldown chunk has no size-limit entry. Add a `codeSplitting.groups` entry in `apps/web/vite.config.ts` to give it a stable name (or absorb it into the `shared` / `api-client` group), then add a matching entry to `.size-limit.json`.

**Adding a new route:**
1. Add a `codeSplitting.groups` entry in `apps/web/vite.config.ts` routing its feature folder to a named chunk.
2. Run `make check-bundle-size` locally to build and measure.
3. Add an entry to `.size-limit.json` with `limit` = measured gzipped size + 20%.

**Local check:** `make check-bundle-size` (builds frontend, runs orphan guard, enforces all budgets).

**Claude Code pre-push hook:** The Claude Code hook at `.claude/hooks/pre-push-checks.sh` invokes `make check-bundle-size` on every push that touches frontend files, enforcing both size-limit budgets AND the orphan-chunk guard automatically.

**Current footprints** — baseline established post-#1225 (Vite 8 / Rolldown `codeSplitting.groups` migration).
**Measured on `feature/1652-revenuecat-centralization` (2026-09-16, merge of `96ae1a53` into `a1ba0503`) via `make check-bundle-size` — every value re-measured in one run.** This table is a point-in-time snapshot, not a live check — if it's older than the last budget or dependency change, re-run `make check-bundle-size` and update every value (and this stamp) before trusting it. Do not hand-edit individual numbers without re-measuring the whole table in one run.

| Chunk | Budget | Actual (gzip) | Headroom |
|-------|--------|---------------|----------|
| `rolldown-runtime` | 5 kB | 422 B | 4.58 kB |
| `preload-helper` | 3 kB | 681 B | 2.32 kB |
| `entry` | 20 kB | 14.5 kB | 5.5 kB |
| `vendor` | 460 kB | 390.18 kB | 69.82 kB |
| `revenuecat` | 264 kB | 219.72 kB | 44.28 kB |
| `charts` | 230 kB | 223.38 kB | 6.62 kB |
| `api-client` | 10 kB | 8.39 kB | 1.61 kB |
| `shared` | 56 kB | 45.88 kB | 10.12 kB |
| `posthog` | 117 kB | 95.8 kB | 21.2 kB |
| `layout-chrome` | 2.5 kB | 2.2 kB | 0.3 kB |
| `header` | 3.6 kB | 3.27 kB | 0.33 kB |
| `admin` | 30 kB | 24.96 kB | 5.04 kB |
| `accounts` | 20 kB | 16.48 kB | 3.52 kB |
| `auto-categorization` | 18.5 kB | 15.54 kB | 2.96 kB |
| `billing` | 15 kB | 8.84 kB | 6.16 kB |
| `budget-plan` | 4.6 kB | 3.82 kB | 0.78 kB |
| `budgets` | 25 kB | 23.62 kB | 1.38 kB |
| `dashboard` | 20 kB | 16.37 kB | 3.63 kB |
| `legal` | 20 kB | 18.23 kB | 1.77 kB |
| `net-worth` | 6 kB | 4.71 kB | 1.29 kB |
| `profile` | 12 kB | 9.9 kB | 2.1 kB |
| `reports` | 7.5 kB | 7.33 kB | 0.17 kB |
| `boosts` | 22 kB | 18.76 kB | 3.24 kB |
| `household` | 9 kB | 8.58 kB | 0.42 kB |
| `mcp` | 13 kB | 8.41 kB | 4.59 kB |
| `savings` | 12 kB | 11.46 kB | 0.54 kB |
| `transactions` | 22 kB | 18.05 kB | 3.95 kB |
| `custom-category-manager` | 7 kB | 3.71 kB | 3.29 kB |
| `category-editor` | 4 kB | 2.83 kB | 1.17 kB |
| `css` | 68 kB | 23.25 kB | 44.75 kB |
| `planning` | 2 kB | 685 B | 1.32 kB |
| `insights` | 2 kB | 753 B | 1.25 kB |
| `investments` | 8.5 kB | 7.49 kB | 1.01 kB |

*`rolldown-runtime`, `preload-helper`, and `css` are framework/runtime and global-stylesheet budgets, not per-feature or vendor-family chunks. All three sit at ≤35% of their limit as of the same measurement and remain enforced in `.size-limit.json`.*
*`layout-chrome` and `header` are newly LISTED, not newly created: both are real chunks with real budgets in `.size-limit.json` that the previous stamp (2026-08-11) simply predates. An absent row here never meant an absent budget — the orphan-chunk guard, not this table, is what makes a budget mandatory.*
*The `tanstack` and `rules` rows from earlier revisions of this table no longer correspond to real chunks: the `tanstack` split was reverted in #1288 (folded back into `vendor`), and `rules` was renamed to `boosts` in #1283.*

Budgets were significantly tightened in issue #1225 after migrating from Vite 7 (Rollup `manualChunks`) to Vite 8 (Rolldown `codeSplitting.groups`). The new chunk taxonomy split node_modules more precisely: an isolated `tanstack` chunk (~44 kB) and `charts` (~215 kB) were carved out of the `vendor` catch-all, briefly taking `vendor` down to 357 kB gzip. That reduction didn't stick: the `tanstack` split was reverted in #1288 ("Fix staging blank page — drop Rolldown tanstack chunk split") for chunk-init stability, folding `@tanstack/*` back into `vendor`, and subsequent dependency growth pushed it further. `vendor` now measures 390.18 kB gzip against a 460 kB budget after #1650 moved the lazy `posthog` SDK out of it — see the stamped table above for current, single-source-of-truth numbers; don't reuse the 357 kB, 428.36 kB or 449.20 kB figures as current values. Two new source-level chunks — `shared` (app-wide utilities/hooks/UI primitives) and `api-client` (generated React Query hooks) — consolidate what would otherwise be dozens of small orphan chunks.

## Public pages and agent discovery

The public pages (routable signed-out), the Rolldown chunk each is assigned to in `apps/web/vite.config.ts`, and the test that guards it. A page whose source path matches no `codeSplitting.groups` entry lands in the eagerly-imported `entry` chunk, so an absent group name below means `entry`, not "ungrouped".

| Route | Chunk group | Guard test |
|-------|-------------|------------|
| `/` | `entry` (LandingPage is imported eagerly) | `src/features/landing/LandingPage.test.tsx` |
| `/pricing` | `billing` (`src/features/pricing/`) | `src/features/pricing/PricingPage.test.tsx` |
| `/mcp` | `mcp` (`src/features/mcp/`) | `src/features/mcp/McpPage.test.tsx`, `src/features/mcp/clientNaming.test.ts` |
| `/features` | `features` (`src/features/features/`) | `src/features/features/FeaturesPage.test.tsx` |
| `/faq` | `faq` (`src/features/faq/`) | `src/features/faq/FaqPage.test.tsx` |
| `/legal/privacy-policy` | `legal` (`src/legal/`) | `src/legal/PrivacyPolicy.test.tsx` |
| `/legal/terms` | `legal` (`src/legal/`) | `src/legal/TermsOfService.test.tsx` |
| `/legal/cookie-policy` | `legal` (`src/legal/`) | `src/legal/CookiePolicy.test.tsx` |

Two guards cut across the whole set. `src/test/seo-statics.test.ts` pins `public/sitemap.xml`, `public/robots.txt`, `public/llms.txt`, the `index.html` metadata, and the sync between `robots.txt`'s `Disallow` list and the `<ProtectedRoute>` routes in `src/App.tsx`. `src/features/landing/data/marketingContent.drift.test.ts` pins the generated `public/llms-full.txt` to a byte-for-byte recompute of its source prose.

**Adding a public page means updating three lists that must agree:** the route in `src/App.tsx`, the `<loc>` in `public/sitemap.xml`, and a link in `public/llms.txt` (then regenerate `llms-full.txt`). `seo-statics.test.ts` fails if a sitemap URL is missing from `llms.txt`, so the sitemap and the agent index cannot drift apart silently.

**Stamp:** the route/chunk/test mapping above was verified on 2026-09-23 against #1666. It is a point-in-time snapshot, mirroring the bundle-size table above: re-derive a row rather than trusting the stamp if the route, its chunk group, or its guard test changes. The discovery files exist as agent-readable documentation only; see `docs/decisions/2026-09-23-agent-discovery-is-documentation-not-seo.md` for what they are and are not claimed to do.
