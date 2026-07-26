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
- The shared `vendor` chunk sits at **~99.5% of its 410 kB limit** (407.97 kB measured, ~2 kB headroom — see the stamped table below). Treat `vendor` as budget-saturated, not comfortable: with only ~2 kB of headroom left, a new dependency whose gzipped contribution exceeds that headroom will fail CI — and, because of the shared-chunk model, it will fail on whichever PR happens to push it over rather than the one that used up the room. Raising the limit requires a measured before/after in the PR description — and measure on a clean `bun install`, because a stale `node_modules` can carry duplicate copies of a package and inflate this figure by double digits.
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
**Measured at commit `57814af9` (2026-07-26) via `make check-bundle-size`.** This table is a point-in-time snapshot, not a live check — if it's older than the last budget or dependency change, re-run `make check-bundle-size` and update every value (and this stamp) before trusting it. Do not hand-edit individual numbers without re-measuring the whole table in one run.

| Chunk | Budget | Actual (gzip) | Headroom |
|-------|--------|---------------|----------|
| `vendor` | 410 kB | 407.97 kB | 2.03 kB |
| `charts` | 230 kB | 222.44 kB | 7.56 kB |
| `shared` | 40 kB | 35.62 kB | 4.38 kB |
| `api-client` | 10 kB | 6.86 kB | 3.14 kB |
| `entry` | 20 kB | 16.76 kB | 3.24 kB |
| `budgets` | 25 kB | 20.02 kB | 4.98 kB |
| `dashboard` | 20 kB | 16.41 kB | 3.59 kB |
| `admin` | 20 kB | 13.98 kB | 6.02 kB |
| `legal` | 20 kB | 15.83 kB | 4.17 kB |
| `boosts` | 22 kB | 17.65 kB | 4.35 kB |
| `accounts` | 20 kB | 12.05 kB | 7.95 kB |
| `auto-categorization` | 15 kB | 11.17 kB | 3.83 kB |
| `billing` | 15 kB | 10.88 kB | 4.12 kB |
| `transactions` | 22 kB | 14.84 kB | 7.16 kB |
| `profile` | 12 kB | 9.07 kB | 2.93 kB |
| `savings` | 9 kB | 8.62 kB | 0.38 kB |
| `net-worth` | 6 kB | 4.59 kB | 1.41 kB |
| `reports` | 6 kB | 5.6 kB | 0.4 kB |
| `household` | 9 kB | 8.2 kB | 0.8 kB |
| `mcp` | 13 kB | 10.13 kB | 2.87 kB |
| `custom-category-manager` | 7 kB | 3.44 kB | 3.56 kB |
| `category-editor` | 4 kB | 1.67 kB | 2.33 kB |
| `planning` | 2 kB | 0.73 kB | 1.27 kB |
| `insights` | 2 kB | 0.79 kB | 1.21 kB |
| `investments` | 6 kB | 5.77 kB | 0.23 kB |

*Excludes `rolldown-runtime`, `preload-helper`, and `css` — framework/runtime and global-stylesheet budgets, not per-feature or vendor-family chunks. All three sit at ≤34% of their limit as of the same measurement, so they're omitted here as low-risk; they're still enforced in `.size-limit.json`.*
*The `tanstack` and `rules` rows from earlier revisions of this table no longer correspond to real chunks: the `tanstack` split was reverted in #1288 (folded back into `vendor`), and `rules` was renamed to `boosts` in #1283.*

Budgets were significantly tightened in issue #1225 after migrating from Vite 7 (Rollup `manualChunks`) to Vite 8 (Rolldown `codeSplitting.groups`). The new chunk taxonomy split node_modules more precisely: an isolated `tanstack` chunk (~44 kB) and `charts` (~215 kB) were carved out of the `vendor` catch-all, briefly taking `vendor` down to 357 kB gzip. That reduction didn't stick: the `tanstack` split was reverted in #1288 ("Fix staging blank page — drop Rolldown tanstack chunk split") for chunk-init stability, folding `@tanstack/*` back into `vendor`, and subsequent dependency growth pushed it further. `vendor` now measures 407.97 kB gzip against a 410 kB budget — see the stamped table above for current, single-source-of-truth numbers; don't reuse the 357 kB figure as a current value. Two new source-level chunks — `shared` (app-wide utilities/hooks/UI primitives) and `api-client` (generated React Query hooks) — consolidate what would otherwise be dozens of small orphan chunks.
