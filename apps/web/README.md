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
- The shared `vendor` chunk is budgeted at baseline + ~10% (tighter than before; Rolldown produces stable output).
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

**Current footprints (post-#1225, Vite 8 / Rolldown codeSplitting.groups migration):**
| Chunk | Budget | Actual (gzip) | Headroom |
|-------|--------|---------------|----------|
| `vendor` | 400 kB | 357 kB | 43 kB |
| `tanstack` | 55 kB | 44 kB | 11 kB |
| `charts` | 230 kB | 215 kB | 15 kB |
| `shared` | 40 kB | 31 kB | 9 kB |
| `api-client` | 10 kB | 5 kB | 5 kB |
| `entry` | 20 kB | 15 kB | 5 kB |
| `budgets` | 25 kB | 20 kB | 5 kB |
| `dashboard` | 20 kB | 14 kB | 6 kB |
| `admin` | 20 kB | 14 kB | 6 kB |
| `legal` | 20 kB | 15 kB | 5 kB |
| `rules` | 16 kB | 13 kB | 3 kB |
| `accounts` | 20 kB | 11 kB | 9 kB |
| `auto-categorization` | 15 kB | 10 kB | 5 kB |
| `billing` | 15 kB | 11 kB | 4 kB |
| `transactions` | 22 kB | 10 kB | 12 kB |
| `profile` | 12 kB | 8 kB | 4 kB |
| `savings` | 9 kB | 7 kB | 2 kB |
| `net-worth` | 6 kB | 5 kB | 1 kB |
| `reports` | 6 kB | 4 kB | 2 kB |
| `household` | 9 kB | 7.66 kB | 1.34 kB |
| `mcp` | 13 kB | 10.14 kB | 2.86 kB |

Budgets were significantly tightened in issue #1225 after migrating from Vite 7 (Rollup `manualChunks`) to Vite 8 (Rolldown `codeSplitting.groups`). The new chunk taxonomy splits node_modules more precisely: `tanstack` (~44 kB) and `charts` (~215 kB) are now isolated from the `vendor` catch-all, reducing `vendor` from 700 kB to 357 kB gzip. Two new source-level chunks — `shared` (app-wide utilities/hooks/UI primitives) and `api-client` (generated React Query hooks) — consolidate what would otherwise be dozens of small orphan chunks.
