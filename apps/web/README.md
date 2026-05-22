# apps/web

React 19 / TypeScript 5 / Vite 7 frontend for BoostedBudget.

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
- Emits a new Rollup chunk with no matching budget entry ("orphan chunk")

**Budget policy:**
- Per-feature chunks are budgeted at their gzipped baseline + 20%.
- The shared `vendor` chunk is budgeted at baseline + 25% (looser, to absorb routine Dependabot dependency churn without flapping).
- Initial budgets were set at the time lazy-loading was introduced (issue #1126).

**If CI fails on bundle size:**
1. **Chunk exceeded its budget** — either shrink the chunk back below budget, or raise the budget in the same PR with a reasoned justification in the PR description (e.g., "added echarts component, +X KB gzipped").
2. **Orphan chunk detected** — a new Rollup chunk has no size-limit entry. Add a `manualChunks` branch in `apps/web/vite.config.ts` to give it a stable name, then add a matching entry to `.size-limit.json`.

**Adding a new route:**
1. Add a `manualChunks` branch in `apps/web/vite.config.ts` routing its feature folder to a named chunk.
2. Run `make check-bundle-size` locally to build and measure.
3. Add an entry to `.size-limit.json` with `limit` = measured gzipped size + 20%.

**Local check:** `make check-bundle-size` (builds frontend, runs orphan guard, enforces all budgets).

**Claude Code pre-push hook:** The Claude Code hook at `.claude/hooks/pre-push-checks.sh` invokes `make check-bundle-size` on every push that touches frontend files, enforcing both size-limit budgets AND the orphan-chunk guard automatically.

**Current footprints (post-#1169, Plotly → ECharts migration):**
| Chunk | Budget | Actual (gzip) | Headroom |
|-------|--------|---------------|----------|
| `vendor` | 700 kB | 616.72 kB | 83.28 kB |
| `transactions` | 19 kB | 14.83 kB | 4.17 kB |
| `tanstack` | 35 kB | 15.03 kB | 19.97 kB |

The `vendor` budget was reduced from 911 kB to 700 kB after issue #1169 (Plotly → Apache ECharts): ECharts tree-shaken via `echarts/core` weighs ~617 kB gzip vs Plotly's ~911 kB budget — a **~294 kB (32%) reduction**.

`@tanstack/react-virtual` is bundled into the shared `tanstack` chunk (not the `transactions` chunk). The transactions chunk budget of 19 kB remains sufficient with ~4.2 kB headroom.
