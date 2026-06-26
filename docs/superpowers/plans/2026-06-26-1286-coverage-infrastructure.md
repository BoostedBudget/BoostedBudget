# Issue #1286: Coverage Infrastructure, Gate Consolidation & Docs (Plan A)

**Goal:** Fix coverage measurement scope (exclude generated code/entrypoints), consolidate duplicate gate implementations, harden CI locale handling, raise enforced floors, and reconcile documentation drift.

**Architecture:** Two shared scripts (`scripts/check-coverage.sh`, `scripts/check-frontend-coverage.sh`) centralize all threshold logic. Both CI (`test.yml`) and Makefile delegate to these scripts. Backend scope narrowed via `-coverpkg=./internal/...,./cmd/plaid_validation/validators/...` (excludes pure entrypoints). Frontend scope narrowed by filtering `packages/api-client/**` (orval-generated) from merged lcov.

**Enforced Floors:** Backend ≥69% statements (scoped), Frontend ≥82% lines (post-filter).

## Implementation Summary

| Task | Files Changed | Changes | Commit |
|------|---------------|---------|--------|
| **Task 2** | `scripts/check-coverage.sh` | Replace bc/integer-truncation with awk decimal-safe comparison (lines 56–77) | `#1286: fix integer-truncation in check-coverage.sh — use awk decimal comparison` |
| **Task 3** | `scripts/check-frontend-coverage.sh` | Create new script: merge lcov groups, filter `packages/api-client/**`, enforce Lines threshold via awk comparison. Locale hardened (`LC_ALL=C LANG=C`). | `#1286: add check-frontend-coverage.sh — merge lcov groups, filter api-client, enforce threshold` |
| **Task 4** | `.github/workflows/test.yml` | (1) Backend Go test: add `-coverpkg=./internal/...,./cmd/plaid_validation/validators/...` (line ~530). (2) Backend gate: delegate to `scripts/check-coverage.sh` with threshold ≥69% (line ~534). | `#1286: scope backend coverage denominator via -coverpkg, raise floor to 69%, use shared script` |
| **Task 5** | `.github/workflows/test.yml` | (1) Add `LC_ALL=C LANG=C` env to lcov steps (~628). (2) Frontend gate: delegate to `scripts/check-frontend-coverage.sh` with threshold ≥82% (line ~644–711). Drop dead Functions/Branches gates. | `#1286: simplify frontend coverage gate — delegate to script, filter api-client, locale-harden, drop dead fn/branch gates` |
| **Task 6** | `Makefile` | Update `test-ci` (69–76), `test-fast` (98–109), `test-all` (157–189): add `-coverpkg` scoping, use `scripts/check-coverage.sh` (69%), add frontend check (82%) to test-all. | `#1286: consolidate Makefile coverage gates into shared scripts, scope -coverpkg, add frontend threshold to test-all` |
| **Task 7** | `README.md` `CLAUDE.md` | Fix CI badge (ci-cd.yml → test.yml), Contributing section coverage (≥69% backend, ≥82% frontend), CLAUDE.md floors (same). | `#1286: fix README CI badge (ci-cd.yml → test.yml), reconcile coverage threshold docs with enforced floors` |

## Key Decisions

- **Backend floor: 69%** (was incorrectly estimated as 68%; CI enforces 69%)
- **Frontend floor: 82%** (was incorrectly estimated as 74%; CI enforces 82%)
- **Shared scripts are ground truth:** All gate logic lives in `scripts/check-coverage.sh` and `scripts/check-frontend-coverage.sh`. CI and Makefile delegate to these — no more inline duplicates.
- **Floor philosophy:** Set just below measured post-exclusion actuals; Plans B & C will ratchet further toward higher targets.
- **Locale hardening:** Both scripts and CI steps set `LC_ALL=C LANG=C` to prevent lcov panics on macOS.
- **Generated code exclusion:** Backend via `-coverpkg` (entrypoints), frontend via lcov filter (api-client).
- **Decimal-safe comparison:** All scripts use awk, not bc or integer truncation.
- **Dead gates removed:** Functions/Branches gates dropped from CI (Bun lcov emits no data).

## Plans B, C, D Sequencing

**Plan B — Backend test campaign** *(after Plan A merges)*
Ratchet 69% → 72% → 76% → 80% via critical-path packages.

**Plan C — Frontend test campaign** *(parallel with Plan B)*
Ratchet 82% → 85% → 87% → 90%.

**Plan D — Finalize gates** *(after B and C land)*
Set both to 90% (or measured maximum). Update CLAUDE.md.
