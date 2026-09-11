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
- The shared `vendor` chunk sits at **~97.7% of its 460 kB limit** (449.30 kB measured, 10.70 kB headroom — see the stamped table below). ⚠ Until #1625 re-measured it, this bullet described a **435 kB limit that no longer existed** and a measurement two dependency waves stale. Both the limit and the actual drift; re-read them off the stamped table rather than this sentence. Treat `vendor` as budget-saturated, not comfortable: with only ~6 kB of headroom left, a new dependency whose gzipped contribution exceeds that headroom will fail CI — and, because of the shared-chunk model, it will fail on whichever PR happens to push it over rather than the one that used up the room. Raising the limit requires a measured before/after in the PR description — and measure on a clean `bun install`, because a stale `node_modules` can carry duplicate copies of a package and inflate this figure by double digits.
- The limit was last raised **410 → 435 kB in #1513**, the consolidated Dependabot bump. Attribution was measured by bisecting in a clean worktree off `staging`: baseline 408.32 kB, final 428.36 kB, and essentially all of the +20 kB came from the eight-package `@radix-ui/*` group (PR #1317) — gridstack 12→13 contributed ~0.7 kB. The growth is spread across the group rather than concentrated: applied individually, `react-dialog` costs +6.1 kB, the other six +8.0 kB and `react-avatar` +0.9 kB, but applied together they cost +19.3 kB, because their shared internals dedupe worse across eight simultaneously-bumped packages. There was no single package to hold back. **If `vendor` needs to come back down, the lever is splitting `@radix-ui/*` into its own lazy chunk** via a `codeSplitting.groups` entry plus a matching `.size-limit.json` budget — deliberately not done in #1513 to keep that PR dependency-only.
- `admin` was raised **20 → 24 kB in #1631**. Measured before/after in the same clean run: 18.97 kB → 20.26 kB, +1.29 kB gzipped, for the structured DLQ error view (`JobErrorList` + `parseJobErrors`, which replaced a one-line JSON dump with the Postgres context an operator needs). The old 20 kB limit had only 1.02 kB of headroom, so the feature cleared it by 262 B; the new limit is the measured size + 20% per the policy above rather than a nudge past the failure, which would have left the next admin change to fail for this one's reasons.
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
**Measured at commit `36a663d4` (2026-09-11, #1631) via `make check-bundle-size` — every value re-measured in one run.** This table is a point-in-time snapshot, not a live check — if it's older than the last budget or dependency change, re-run `make check-bundle-size` and update every value (and this stamp) before trusting it. Do not hand-edit individual numbers without re-measuring the whole table in one run.

| Chunk | Budget | Actual (gzip) | Headroom |
|-------|--------|---------------|----------|
| `rolldown-runtime` | 5 kB | 0.42 kB | 4.58 kB |
| `preload-helper` | 3 kB | 0.68 kB | 2.32 kB |
| `entry` | 20 kB | 14.56 kB | 5.44 kB |
| `vendor` | 460 kB | 449.30 kB | 10.70 kB |
| `charts` | 230 kB | 223.14 kB | 6.86 kB |
| `api-client` | 10 kB | 7.94 kB | 2.06 kB |
| `shared` | 40 kB | 38.35 kB | 1.65 kB |
| `layout-chrome` | 2.5 kB | 2.21 kB | 0.29 kB |
| `header` | 3.6 kB | 3.28 kB | 0.32 kB |
| `admin` | 24 kB | 20.29 kB | 3.71 kB |
| `accounts` | 20 kB | 16.49 kB | 3.51 kB |
| `auto-categorization` | 18.5 kB | 15.54 kB | 2.96 kB |
| `billing` | 15 kB | 11.46 kB | 3.54 kB |
| `budgets` | 25 kB | 21.18 kB | 3.82 kB |
| `dashboard` | 20 kB | 16.37 kB | 3.63 kB |
| `legal` | 20 kB | 17.45 kB | 2.55 kB |
| `net-worth` | 6 kB | 4.71 kB | 1.29 kB |
| `profile` | 12 kB | 9.39 kB | 2.61 kB |
| `reports` | 7.5 kB | 7.33 kB | 0.17 kB |
| `boosts` | 22 kB | 18.77 kB | 3.23 kB |
| `household` | 9 kB | 8.58 kB | 0.42 kB |
| `mcp` | 13 kB | 8.41 kB | 4.59 kB |
| `savings` | 12 kB | 11.33 kB | 0.67 kB |
| `transactions` | 22 kB | 15.87 kB | 6.13 kB |
| `custom-category-manager` | 7 kB | 3.72 kB | 3.28 kB |
| `category-editor` | 4 kB | 1.64 kB | 2.36 kB |
| `css` | 68 kB | 23.14 kB | 44.86 kB |
| `planning` | 2 kB | 0.69 kB | 1.31 kB |
| `insights` | 2 kB | 0.75 kB | 1.25 kB |
| `investments` | 8.5 kB | 7.50 kB | 1.00 kB |

*Excludes `rolldown-runtime`, `preload-helper`, and `css` — framework/runtime and global-stylesheet budgets, not per-feature or vendor-family chunks. All three sit at ≤35% of their limit as of the same measurement, so they're omitted here as low-risk; they're still enforced in `.size-limit.json`.*
*`layout-chrome` and `header` are newly LISTED, not newly created: both are real chunks with real budgets in `.size-limit.json` that the previous stamp (2026-08-11) simply predates. An absent row here never meant an absent budget — the orphan-chunk guard, not this table, is what makes a budget mandatory.*
*The `tanstack` and `rules` rows from earlier revisions of this table no longer correspond to real chunks: the `tanstack` split was reverted in #1288 (folded back into `vendor`), and `rules` was renamed to `boosts` in #1283.*

Budgets were significantly tightened in issue #1225 after migrating from Vite 7 (Rollup `manualChunks`) to Vite 8 (Rolldown `codeSplitting.groups`). The new chunk taxonomy split node_modules more precisely: an isolated `tanstack` chunk (~44 kB) and `charts` (~215 kB) were carved out of the `vendor` catch-all, briefly taking `vendor` down to 357 kB gzip. That reduction didn't stick: the `tanstack` split was reverted in #1288 ("Fix staging blank page — drop Rolldown tanstack chunk split") for chunk-init stability, folding `@tanstack/*` back into `vendor`, and subsequent dependency growth pushed it further. `vendor` now measures 449.20 kB gzip against a 460 kB budget — see the stamped table above for current, single-source-of-truth numbers; don't reuse the 357 kB or 428.36 kB figures as current values. Two new source-level chunks — `shared` (app-wide utilities/hooks/UI primitives) and `api-client` (generated React Query hooks) — consolidate what would otherwise be dozens of small orphan chunks.
