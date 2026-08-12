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
- The shared `vendor` chunk sits at **~98.5% of its 435 kB limit** (428.36 kB measured, ~6.6 kB headroom — see the stamped table below). Treat `vendor` as budget-saturated, not comfortable: with only ~6 kB of headroom left, a new dependency whose gzipped contribution exceeds that headroom will fail CI — and, because of the shared-chunk model, it will fail on whichever PR happens to push it over rather than the one that used up the room. Raising the limit requires a measured before/after in the PR description — and measure on a clean `bun install`, because a stale `node_modules` can carry duplicate copies of a package and inflate this figure by double digits.
- The limit was last raised **410 → 435 kB in #1513**, the consolidated Dependabot bump. Attribution was measured by bisecting in a clean worktree off `staging`: baseline 408.32 kB, final 428.36 kB, and essentially all of the +20 kB came from the eight-package `@radix-ui/*` group (PR #1317) — gridstack 12→13 contributed ~0.7 kB. The growth is spread across the group rather than concentrated: applied individually, `react-dialog` costs +6.1 kB, the other six +8.0 kB and `react-avatar` +0.9 kB, but applied together they cost +19.3 kB, because their shared internals dedupe worse across eight simultaneously-bumped packages. There was no single package to hold back. **If `vendor` needs to come back down, the lever is splitting `@radix-ui/*` into its own lazy chunk** via a `codeSplitting.groups` entry plus a matching `.size-limit.json` budget — deliberately not done in #1513 to keep that PR dependency-only.
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
**Measured at commit `6b577dce` (2026-08-11, #1513) via `make check-bundle-size`.** This table is a point-in-time snapshot, not a live check — if it's older than the last budget or dependency change, re-run `make check-bundle-size` and update every value (and this stamp) before trusting it. Do not hand-edit individual numbers without re-measuring the whole table in one run.

| Chunk | Budget | Actual (gzip) | Headroom |
|-------|--------|---------------|----------|
| `vendor` | 435 kB | 428.36 kB | 6.64 kB |
| `charts` | 230 kB | 222.44 kB | 7.56 kB |
| `shared` | 40 kB | 35.94 kB | 4.06 kB |
| `api-client` | 10 kB | 7.33 kB | 2.67 kB |
| `entry` | 20 kB | 16.73 kB | 3.27 kB |
| `budgets` | 25 kB | 20.01 kB | 4.99 kB |
| `dashboard` | 20 kB | 16.4 kB | 3.6 kB |
| `admin` | 20 kB | 19.12 kB | 0.88 kB |
| `legal` | 20 kB | 15.83 kB | 4.17 kB |
| `boosts` | 22 kB | 17.63 kB | 4.37 kB |
| `accounts` | 20 kB | 12.07 kB | 7.93 kB |
| `auto-categorization` | 15 kB | 11.18 kB | 3.82 kB |
| `billing` | 15 kB | 11.87 kB | 3.13 kB |
| `transactions` | 22 kB | 14.85 kB | 7.15 kB |
| `profile` | 12 kB | 9.09 kB | 2.91 kB |
| `savings` | 9 kB | 8.61 kB | 0.39 kB |
| `net-worth` | 6 kB | 4.59 kB | 1.41 kB |
| `reports` | 6 kB | 5.6 kB | 0.4 kB |
| `household` | 9 kB | 8.19 kB | 0.81 kB |
| `mcp` | 13 kB | 8.29 kB | 4.71 kB |
| `custom-category-manager` | 7 kB | 3.43 kB | 3.57 kB |
| `category-editor` | 4 kB | 1.67 kB | 2.33 kB |
| `planning` | 2 kB | 0.72 kB | 1.28 kB |
| `insights` | 2 kB | 0.79 kB | 1.21 kB |
| `investments` | 6 kB | 5.77 kB | 0.23 kB |

*Excludes `rolldown-runtime`, `preload-helper`, and `css` — framework/runtime and global-stylesheet budgets, not per-feature or vendor-family chunks. All three sit at ≤35% of their limit as of the same measurement, so they're omitted here as low-risk; they're still enforced in `.size-limit.json`.*
*The `tanstack` and `rules` rows from earlier revisions of this table no longer correspond to real chunks: the `tanstack` split was reverted in #1288 (folded back into `vendor`), and `rules` was renamed to `boosts` in #1283.*

Budgets were significantly tightened in issue #1225 after migrating from Vite 7 (Rollup `manualChunks`) to Vite 8 (Rolldown `codeSplitting.groups`). The new chunk taxonomy split node_modules more precisely: an isolated `tanstack` chunk (~44 kB) and `charts` (~215 kB) were carved out of the `vendor` catch-all, briefly taking `vendor` down to 357 kB gzip. That reduction didn't stick: the `tanstack` split was reverted in #1288 ("Fix staging blank page — drop Rolldown tanstack chunk split") for chunk-init stability, folding `@tanstack/*` back into `vendor`, and subsequent dependency growth pushed it further. `vendor` now measures 428.36 kB gzip against a 435 kB budget — see the stamped table above for current, single-source-of-truth numbers; don't reuse the 357 kB figure as a current value. Two new source-level chunks — `shared` (app-wide utilities/hooks/UI primitives) and `api-client` (generated React Query hooks) — consolidate what would otherwise be dozens of small orphan chunks.
