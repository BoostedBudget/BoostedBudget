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
- The shared `vendor` chunk sits at **~97.6% of its 460 kB limit** (449.01 kB measured, 10.99 kB headroom — see the stamped table below). Treat it as budget-saturated: a dependency whose gzipped contribution exceeds that headroom will fail CI. Raising the limit requires a measured before/after in the PR description — and measure on a clean `bun install`, because a stale `node_modules` can carry duplicate copies of a package and inflate this figure by double digits.
- The limit was last raised **410 → 435 kB in #1513**, the consolidated Dependabot bump. Attribution was measured by bisecting in a clean worktree off `staging`: baseline 408.32 kB, final 428.36 kB, and essentially all of the +20 kB came from the eight-package `@radix-ui/*` group (PR #1317) — gridstack 12→13 contributed ~0.7 kB. The growth is spread across the group rather than concentrated: applied individually, `react-dialog` costs +6.1 kB, the other six +8.0 kB and `react-avatar` +0.9 kB, but applied together they cost +19.3 kB, because their shared internals dedupe worse across eight simultaneously-bumped packages. There was no single package to hold back. **If `vendor` needs to come back down, the lever is splitting `@radix-ui/*` into its own lazy chunk** via a `codeSplitting.groups` entry plus a matching `.size-limit.json` budget — deliberately not done in #1513 to keep that PR dependency-only.
- `admin` was raised **20 → 24 kB in #1631**. Measured before/after in the same clean run: 18.97 kB → 20.26 kB, +1.29 kB gzipped, for the structured DLQ error view (`JobErrorList` + `parseJobErrors`, which replaced a one-line JSON dump with the Postgres context an operator needs). The old 20 kB limit had only 1.02 kB of headroom, so the feature cleared it by 262 B; the new limit is the measured size + 20% per the policy above rather than a nudge past the failure, which would have left the next admin change to fail for this one's reasons.
- #1637/#1627 adds a detail-only `budget-plan` chunk: 3.76 kB gzip, budgeted at 4.6 kB (measured size + 20%, rounded). A higher-priority group preserves the panel's lazy boundary; the feature-wide rule had absorbed it into `budgets`, measuring 26.24 kB against 25 kB. After splitting, `budgets` measures 23.33 kB and keeps its existing limit. No dependencies were added. The landing route remains 533 KiB against its 542 KiB limit.
- #1652 isolates `@revenuecat/purchases-js` in the lazy `revenuecat` chunk. The existing vendor catch-all absorbed the SDK despite its dynamic import, measuring 670.29 kB. A higher-priority group reduces vendor to 449.01 kB; the SDK measures 219.715 kB and has a 264 kB budget (baseline + 20%, rounded). The vendor limit stays 460 kB. A production smoke test requires the separate SDK chunk and verifies `/` does not request it.
- #1652 also raises `shared` **40 → 49 kB** and `admin` **24 → 30 kB**, using measured baselines + 20%, rounded. Shared measures 40.327 kB (previous stamped baseline 38.59 kB), including the checkout identity/cancellation controller and application-wide confirmation hook. Admin measures 24.948 kB (previous 20.28 kB), including RevenueCat diagnostics, nullable authority/health rendering, grant controls and operation history. The landing route measures **535 KiB against its unchanged 542 KiB limit**; SDK bytes remain outside its initial download.
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
**Measured on `feature/1652-revenuecat-centralization` (2026-09-13, baseline `9b22ed3c` plus #1652 changes) via `make check-bundle-size` — every value re-measured in one run.** This table is a point-in-time snapshot, not a live check — if it's older than the last budget or dependency change, re-run `make check-bundle-size` and update every value (and this stamp) before trusting it. Do not hand-edit individual numbers without re-measuring the whole table in one run.

| Chunk | Budget | Actual (gzip) | Headroom |
|-------|--------|---------------|----------|
| `rolldown-runtime` | 5 kB | 0.42 kB | 4.58 kB |
| `preload-helper` | 3 kB | 0.68 kB | 2.32 kB |
| `entry` | 20 kB | 14.48 kB | 5.52 kB |
| `vendor` | 460 kB | 449.01 kB | 10.99 kB |
| `revenuecat` | 264 kB | 219.72 kB | 44.28 kB |
| `charts` | 230 kB | 223.14 kB | 6.86 kB |
| `api-client` | 10 kB | 8.16 kB | 1.84 kB |
| `shared` | 49 kB | 40.33 kB | 8.67 kB |
| `layout-chrome` | 2.5 kB | 2.20 kB | 0.30 kB |
| `header` | 3.6 kB | 3.27 kB | 0.33 kB |
| `admin` | 30 kB | 24.95 kB | 5.05 kB |
| `accounts` | 20 kB | 16.51 kB | 3.49 kB |
| `auto-categorization` | 18.5 kB | 15.54 kB | 2.96 kB |
| `billing` | 15 kB | 11.68 kB | 3.32 kB |
| `budget-plan` | 4.6 kB | 3.82 kB | 0.78 kB |
| `budgets` | 25 kB | 23.64 kB | 1.36 kB |
| `dashboard` | 20 kB | 16.37 kB | 3.63 kB |
| `legal` | 20 kB | 17.45 kB | 2.55 kB |
| `net-worth` | 6 kB | 4.71 kB | 1.29 kB |
| `profile` | 12 kB | 9.39 kB | 2.61 kB |
| `reports` | 7.5 kB | 7.33 kB | 0.17 kB |
| `boosts` | 22 kB | 18.81 kB | 3.19 kB |
| `household` | 9 kB | 8.59 kB | 0.41 kB |
| `mcp` | 13 kB | 8.42 kB | 4.58 kB |
| `savings` | 12 kB | 11.46 kB | 0.54 kB |
| `transactions` | 22 kB | 18.04 kB | 3.96 kB |
| `custom-category-manager` | 7 kB | 3.72 kB | 3.28 kB |
| `category-editor` | 4 kB | 2.83 kB | 1.17 kB |
| `css` | 68 kB | 23.18 kB | 44.82 kB |
| `planning` | 2 kB | 0.69 kB | 1.31 kB |
| `insights` | 2 kB | 0.75 kB | 1.25 kB |
| `investments` | 8.5 kB | 7.51 kB | 0.99 kB |

*`rolldown-runtime`, `preload-helper`, and `css` are framework/runtime and global-stylesheet budgets, not per-feature or vendor-family chunks. All three sit at ≤35% of their limit as of the same measurement and remain enforced in `.size-limit.json`.*
*`layout-chrome` and `header` are newly LISTED, not newly created: both are real chunks with real budgets in `.size-limit.json` that the previous stamp (2026-08-11) simply predates. An absent row here never meant an absent budget — the orphan-chunk guard, not this table, is what makes a budget mandatory.*
*The `tanstack` and `rules` rows from earlier revisions of this table no longer correspond to real chunks: the `tanstack` split was reverted in #1288 (folded back into `vendor`), and `rules` was renamed to `boosts` in #1283.*

Budgets were significantly tightened in issue #1225 after migrating from Vite 7 (Rollup `manualChunks`) to Vite 8 (Rolldown `codeSplitting.groups`). The new chunk taxonomy split node_modules more precisely: an isolated `tanstack` chunk (~44 kB) and `charts` (~215 kB) were carved out of the `vendor` catch-all, briefly taking `vendor` down to 357 kB gzip. That reduction didn't stick: the `tanstack` split was reverted in #1288 ("Fix staging blank page — drop Rolldown tanstack chunk split") for chunk-init stability, folding `@tanstack/*` back into `vendor`, and subsequent dependency growth pushed it further. `vendor` now measures 449.01 kB gzip against a 460 kB budget — see the stamped table above for current, single-source-of-truth numbers; don't reuse the 357 kB or 428.36 kB figures as current values. Two new source-level chunks — `shared` (app-wide utilities/hooks/UI primitives) and `api-client` (generated React Query hooks) — consolidate what would otherwise be dozens of small orphan chunks.
