# Issue #1286: Coverage Infrastructure, Gate Consolidation & Docs (Plan A)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the coverage measurement scope (exclude generated code / entrypoints from denominator), consolidate the four duplicate gate implementations into shared scripts, harden CI locale handling for `lcov`, raise enforced floors to match post-exclusion actuals, and reconcile drift between documentation and reality.

**Architecture:** Two shared scripts (`scripts/check-coverage.sh` extended; new `scripts/check-frontend-coverage.sh`) own all threshold logic. Both CI (`test.yml`) and Makefile call these scripts — never inline gate logic again. Backend scope is narrowed via `-coverpkg` to `./internal/...` plus `./cmd/plaid_validation/validators/...`. Frontend scope is narrowed by filtering `packages/api-client/**` from the merged lcov after generation.

**Tech Stack:** Go `go tool cover` / lcov / awk / bash. No new Go or TypeScript dependencies.

## Global Constraints

- Branch: `feature/1286-coverage-infrastructure` (`git checkout -b feature/1286-coverage-infrastructure`)
- Commits prefixed `#1286:`
- PR body must include `Closes #1286`
- NO logic changes to application code — only test infrastructure, scripts, CI config, and docs
- Every shell change tested locally before committing: run the new scripts by hand with the checked-in `backend/coverage.out` and `apps/web/coverage/combined.lcov`
- Keep the "floor, not ratchet" philosophy — set floors just below measured post-exclusion actuals; Plans B & C will ratchet further

---

### Task 1: Measure post-exclusion baselines (read-only, no commits)

**Files:**
- No file changes — record output in a scratch file or terminal notes

This task establishes the exact floor values to put in Tasks 4-6. Do it first.

- [ ] **Step 1: Measure scoped backend coverage**

```bash
cd backend
go test -v -race \
  -coverprofile=coverage.out -covermode=atomic \
  -coverpkg=./internal/...,./cmd/plaid_validation/validators/... \
  ./...
go tool cover -func=coverage.out | tail -1
```

Expected: something around `68-71%`. Record the number — call it `BACKEND_ACTUAL`. The floor we set will be `BACKEND_ACTUAL` truncated to the nearest integer minus 1 (so 69.3% → floor 68%).

- [ ] **Step 2: Measure scoped frontend coverage**

```bash
cd apps/web
# Merge all groups
lcov \
  -a coverage/lcov.info \
  -a coverage-conflict/group-a/lcov.info \
  -a coverage-conflict/group-b/lcov.info \
  -a coverage-conflict/group-c/lcov.info \
  -a coverage-conflict/group-d/lcov.info \
  -o /tmp/combined-check.lcov 2>/dev/null || \
lcov -a coverage/lcov.info -o /tmp/combined-check.lcov  # fallback if no conflict groups
# Filter generated api-client code
lcov --remove /tmp/combined-check.lcov '*/packages/api-client/*' \
  -o /tmp/combined-filtered.lcov
# Report
lcov --summary /tmp/combined-filtered.lcov 2>&1 | grep lines
```

Expected: around `72-76%`. Record this as `FRONTEND_LINES_ACTUAL`. Floor = truncate to integer minus 1.

> **If conflict groups are missing** (e.g., running from the checked-in state without a fresh test run), the second number may be absent. That is OK — note it and proceed. The exact floor will be measured again in CI after Phase A changes land.

- [ ] **Step 3: Record your baseline numbers**

Write them down (a `scratch.md` or just terminal notes — do NOT commit):

```
BACKEND_ACTUAL     = ____%   →  BACKEND_FLOOR  = ____% (= int(ACTUAL) - 1)
FRONTEND_LINES_ACTUAL = ____%  →  FRONTEND_FLOOR = ____% (= int(ACTUAL) - 1)
```

Use these floor values in Tasks 4, 5, and 6 below. The plan uses `68` and `74` as defaults based on estimates — adjust if your measurement differs significantly.

---

### Task 2: Fix `scripts/check-coverage.sh` — decimal-safe comparator

**Files:**
- Modify: `scripts/check-coverage.sh` (lines 56-77)
- Test: run by hand against `backend/coverage.out`

The current fallback uses `cut -d. -f1` integer truncation that makes 59.9% pass a 60% gate. Replace with awk decimal arithmetic everywhere, not just when `bc` is installed.

- [ ] **Step 1: Replace the comparison block**

The existing block is:
```bash
# Compare coverage with threshold using bc for float comparison
if command -v bc >/dev/null 2>&1; then
  # Use bc for precise comparison if available
  if (( $(echo "$COVERAGE < $THRESHOLD" | bc -l) )); then
    ...
    exit 1
  fi
else
  # Fallback to integer comparison
  COVERAGE_INT=$(echo "$COVERAGE" | cut -d. -f1)
  THRESHOLD_INT=$(echo "$THRESHOLD" | cut -d. -f1)
  if [ "$COVERAGE_INT" -lt "$THRESHOLD_INT" ]; then
    ...
    exit 1
  fi
fi
```

Replace the entire `if command -v bc` block (lines 57-78) with:
```bash
# Decimal-safe comparison via awk (no external dependency)
if awk -v actual="$COVERAGE" -v threshold="$THRESHOLD" \
       'BEGIN { exit !(actual + 0 < threshold + 0) }'; then
  print_error "Coverage ${COVERAGE}% is below threshold ${THRESHOLD}%"
  echo ""
  echo "Coverage breakdown:"
  go tool cover -func="$COVERAGE_FILE" | grep -v "^mode:" | tail -n 10
  exit 1
fi
```

- [ ] **Step 2: Verify the fix by hand**

```bash
# Should FAIL: 59.9 < 60
echo "mode: atomic" > /tmp/test.out
echo "total: (statements) 59.9%" >> /tmp/test.out
./scripts/check-coverage.sh /tmp/test.out 60
# Expected exit code: 1

# Should PASS: 60.0 >= 60
sed -i '' 's/59.9/60.0/' /tmp/test.out
./scripts/check-coverage.sh /tmp/test.out 60
# Expected exit code: 0

# Sanity check against real profile
./scripts/check-coverage.sh backend/coverage.out 60
# Expected: ✅ pass (coverage is ~65.8%)
```

- [ ] **Step 3: Commit**

```bash
git add scripts/check-coverage.sh
git commit -m "#1286: fix integer-truncation in check-coverage.sh — use awk decimal comparison"
```

---

### Task 3: Create `scripts/check-frontend-coverage.sh`

**Files:**
- Create: `scripts/check-frontend-coverage.sh`
- Test: run by hand against `apps/web/coverage/lcov.info`

A new script that owns the entire frontend coverage workflow: find all lcov groups, merge them, filter generated api-client code, then check the Lines threshold. Centralises the logic that is currently split across `test.yml` (150 lines of inline shell) and only partially present in `Makefile`.

- [ ] **Step 1: Create the script**

```bash
cat > scripts/check-frontend-coverage.sh << 'SCRIPT'
#!/usr/bin/env bash
# check-frontend-coverage.sh — merge frontend lcov groups, filter generated code, enforce threshold
#
# Usage: ./scripts/check-frontend-coverage.sh <web-dir> <lines-threshold>
#   web-dir          Path to apps/web (relative or absolute)
#   lines-threshold  Minimum lines coverage % (integer or decimal, e.g. 74)
#
# Example: ./scripts/check-frontend-coverage.sh apps/web 74

set -euo pipefail

# Locale: lcov panics on non-C locales on some macOS configurations
export LC_ALL=C
export LANG=C

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

print_error()   { echo -e "${RED}❌ $1${NC}"; }
print_success() { echo -e "${GREEN}✅ $1${NC}"; }
print_info()    { echo -e "${YELLOW}ℹ  $1${NC}"; }

WEB_DIR="${1:?Usage: check-frontend-coverage.sh <web-dir> <threshold>}"
THRESHOLD="${2:?Usage: check-frontend-coverage.sh <web-dir> <threshold>}"

# Ensure lcov is available
if ! command -v lcov >/dev/null 2>&1; then
  print_error "lcov is required but not installed. Install with: brew install lcov"
  exit 1
fi

COMBINED="$WEB_DIR/coverage/combined.lcov"
FILTERED="$WEB_DIR/coverage/combined.filtered.lcov"
mkdir -p "$WEB_DIR/coverage"

# --- Merge all lcov groups ---
LCOV_ARGS=""
add_lcov() { [ -f "$1" ] && LCOV_ARGS="$LCOV_ARGS -a $1" || true; }

add_lcov "$WEB_DIR/coverage/lcov.info"
for group in a b c d; do
  add_lcov "$WEB_DIR/coverage-conflict/group-$group/lcov.info"
done

if [ -z "$LCOV_ARGS" ]; then
  print_error "No lcov.info files found under $WEB_DIR/coverage/ or coverage-conflict/. Run bun run test:ci first."
  exit 1
fi

print_info "Merging lcov groups..."
# shellcheck disable=SC2086
lcov $LCOV_ARGS -o "$COMBINED"

# --- Filter generated packages/api-client code ---
# packages/api-client is orval-generated — not unit-testable, should not be in denominator.
# packages/shared-types/matching.ts is hand-written and IS counted (do not exclude it).
print_info "Filtering generated packages/api-client/** from denominator..."
lcov --remove "$COMBINED" '*/packages/api-client/*' -o "$FILTERED"

# --- Parse Lines coverage ---
SUMMARY=$(lcov --summary "$FILTERED" 2>&1)
print_info "Post-filter coverage summary:"
echo "$SUMMARY" | grep -E "(lines|source files)" || true

LINES=$(echo "$SUMMARY" | grep "lines" | grep -oE '[0-9]+([.][0-9]+)?%' | head -1 | sed 's/%//' || true)

if [ -z "$LINES" ]; then
  print_error "Could not parse Lines coverage from lcov summary. Raw output:"
  echo "$SUMMARY"
  exit 1
fi

print_info "Lines coverage: ${LINES}% (threshold: ${THRESHOLD}%)"

# Decimal-safe comparison
if awk -v actual="$LINES" -v threshold="$THRESHOLD" \
       'BEGIN { exit !(actual + 0 < threshold + 0) }'; then
  print_error "Frontend Lines coverage ${LINES}% is below threshold ${THRESHOLD}%"
  exit 1
fi

print_success "Frontend Lines coverage ${LINES}% meets threshold ${THRESHOLD}%"
SCRIPT

chmod +x scripts/check-frontend-coverage.sh
```

- [ ] **Step 2: Verify by hand**

```bash
# Test against the checked-in coverage artifacts (may be slightly stale, that is OK)
./scripts/check-frontend-coverage.sh apps/web 60
# Expected: ✅ pass (well above 60% after filtering api-client)

# Verify the filter is working — file count should be lower in combined.filtered.lcov
grep -c '^SF:' apps/web/coverage/combined.lcov       # should be ~480
grep -c '^SF:' apps/web/coverage/combined.filtered.lcov  # should be ~50 fewer (api-client removed)
```

- [ ] **Step 3: Commit**

```bash
git add scripts/check-frontend-coverage.sh
git commit -m "#1286: add check-frontend-coverage.sh — merge lcov groups, filter api-client, enforce threshold"
```

---

### Task 4: Update `.github/workflows/test.yml` — backend gate

**Files:**
- Modify: `.github/workflows/test.yml` lines 530-549

Two changes: (1) scope backend Go test to real packages via `-coverpkg`; (2) replace the inline integer-truncation comparison with a call to `scripts/check-coverage.sh`.

- [ ] **Step 1: Edit the "Run Go tests with coverage" step (line 530)**

Find:
```yaml
      - name: Run Go tests with coverage
        working-directory: ${{ env.UNIQUE_WORKSPACE }}/backend
        run: go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
```

Replace with (use the `BACKEND_FLOOR` from Task 1 — default 68):
```yaml
      - name: Run Go tests with coverage
        working-directory: ${{ env.UNIQUE_WORKSPACE }}/backend
        run: |
          # -coverpkg scopes the denominator to real testable packages only.
          # cmd/server, cmd/encrypt-migrate, scripts/generate-openapi are entrypoints
          # with no unit tests; including them at 0% drags the total unfairly.
          # cmd/plaid_validation/validators IS real logic and IS included.
          go test -v -race \
            -coverprofile=coverage.out -covermode=atomic \
            -coverpkg=./internal/...,./cmd/plaid_validation/validators/... \
            ./...
```

- [ ] **Step 2: Edit the "Check Go coverage threshold" step (line 534)**

Find (lines 534-549):
```yaml
      - name: Check Go coverage threshold (≥60%)
        working-directory: ${{ env.UNIQUE_WORKSPACE }}/backend
        run: |
          echo "::group::Checking coverage threshold"
          COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
          echo "Current coverage: ${COVERAGE}%"

          COVERAGE_INT=$(echo "$COVERAGE" | cut -d. -f1)

          if [ "$COVERAGE_INT" -lt 60 ]; then
            echo "::error::Coverage ${COVERAGE}% is below threshold 60%"
            exit 1
          else
            echo "::notice::✅ Coverage ${COVERAGE}% meets threshold 60%"
          fi
          echo "::endgroup::"
```

Replace with (use your `BACKEND_FLOOR` — default 68):
```yaml
      - name: Check Go coverage threshold (≥68%)
        working-directory: ${{ env.UNIQUE_WORKSPACE }}/backend
        run: |
          echo "::group::Checking coverage threshold"
          # Delegates to scripts/check-coverage.sh for decimal-safe comparison.
          # Threshold raised from 60% to 68% after scoping denominator to real packages
          # (./internal/... + ./cmd/plaid_validation/validators/...) per issue #1286.
          cd ${{ github.workspace }}
          ./scripts/check-coverage.sh \
            ${{ env.UNIQUE_WORKSPACE }}/backend/coverage.out \
            68
          echo "::endgroup::"
```

> **Note:** The `cd ${{ github.workspace }}` is needed because `working-directory` is set to the backend subdirectory, but the script lives at the repo root. Adjust if the CI layout requires it (alternatively, use an absolute path or run the script relative to `$GITHUB_WORKSPACE`).

Actually, looking at the CI setup more carefully — the `UNIQUE_WORKSPACE` checkout is under `$GITHUB_WORKSPACE/$UNIQUE_WORKSPACE`. Scripts are under that checkout too. Use:

```yaml
      - name: Check Go coverage threshold (≥68%)
        working-directory: ${{ env.UNIQUE_WORKSPACE }}/backend
        run: |
          echo "::group::Checking coverage threshold"
          # Threshold raised from 60% → 68% after -coverpkg scoping (#1286)
          bash ${{ github.workspace }}/${{ env.UNIQUE_WORKSPACE }}/scripts/check-coverage.sh \
            coverage.out \
            68
          echo "::endgroup::"
```

- [ ] **Step 3: Verify the YAML is valid**

```bash
# Check YAML syntax (requires yamllint or python)
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/test.yml'))" && echo "YAML OK"
```

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/test.yml
git commit -m "#1286: scope backend coverage denominator via -coverpkg, raise floor to 68%, use shared script"
```

---

### Task 5: Update `.github/workflows/test.yml` — frontend gate

**Files:**
- Modify: `.github/workflows/test.yml` lines 628-711

Three changes: (1) add locale env vars to the `lcov` steps; (2) replace the 150-line inline merge+filter+threshold shell block with a call to `scripts/check-frontend-coverage.sh`; (3) drop the dead Functions/Branches soft gates.

- [ ] **Step 1: Add locale env to the "Ensure lcov is installed" step (line 628)**

Find:
```yaml
      - name: Ensure lcov is installed
        run: |
          # Self-hosted macOS runners may not have Homebrew in PATH at step launch.
```

Replace `run: |` with:
```yaml
      - name: Ensure lcov is installed
        env:
          LC_ALL: C
          LANG: C
        run: |
          # Self-hosted macOS runners may not have Homebrew in PATH at step launch.
```

- [ ] **Step 2: Replace the "Combine coverage reports and enforce thresholds" step (lines 644-711)**

Find the entire step from `- name: Combine coverage reports and enforce thresholds` through `echo "::endgroup::"` (line 711).

Replace with (use `FRONTEND_FLOOR` from Task 1 — default 74):
```yaml
      - name: Combine coverage reports and enforce thresholds
        working-directory: ${{ env.UNIQUE_WORKSPACE }}/apps/web
        env:
          LC_ALL: C
          LANG: C
        run: |
          echo "::group::Combining coverage reports and checking thresholds"
          # Delegates to scripts/check-frontend-coverage.sh which:
          #   1. Merges all lcov groups (regular + conflict a/b/c/d)
          #   2. Filters packages/api-client/** (orval-generated, not unit-testable)
          #   3. Enforces Lines >= 74% (raised from 63% after excluding generated code, #1286)
          # Functions/Branches gates are removed: Bun LCOV emits no per-fn/branch data,
          # making those checks permanently no-ops that mislead future readers.
          bash ${{ github.workspace }}/${{ env.UNIQUE_WORKSPACE }}/scripts/check-frontend-coverage.sh \
            . \
            74
          echo "::endgroup::"
```

- [ ] **Step 3: Verify the YAML is valid**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/test.yml'))" && echo "YAML OK"
```

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/test.yml
git commit -m "#1286: simplify frontend coverage gate — delegate to script, filter api-client, locale-harden, drop dead fn/branch gates"
```

---

### Task 6: Update `Makefile` — consolidate backend and frontend gates

**Files:**
- Modify: `Makefile` (lines 69-76 `test-ci`, 98-109 `test-fast`, 157-188 `test-all`)

Three `make` targets duplicate the backend coverage gate inline. `test-fast` runs no frontend coverage at all. `test-all` combines lcov but doesn't enforce a threshold. Fix all three to use the shared scripts.

- [ ] **Step 1: Update `test-ci` (lines 69-76)**

Find:
```makefile
test-ci:
	@echo "Running CI tests with coverage enforcement..."
	cd backend && go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Checking backend coverage threshold..."
	./scripts/check-coverage.sh backend/coverage.out 60
	cd apps/web && bun run test:ci
	@echo "All CI tests passed!"
	@echo "Note: combine + threshold enforcement is handled in CI workflow (lcov inline)"
```

Replace with:
```makefile
test-ci:
	@echo "Running CI tests with coverage enforcement..."
	cd backend && go test -v -race \
		-coverprofile=coverage.out -covermode=atomic \
		-coverpkg=./internal/...,./cmd/plaid_validation/validators/... \
		./...
	@echo "Checking backend coverage threshold (≥68%)..."
	./scripts/check-coverage.sh backend/coverage.out 68
	cd apps/web && bun run test:ci
	@echo "Checking frontend coverage threshold (≥74% lines)..."
	./scripts/check-frontend-coverage.sh apps/web 74
	@echo "All CI tests passed!"
```

- [ ] **Step 2: Update `test-fast` backend gate (lines 98-109)**

Find:
```makefile
	@cd backend && go test ./... -race -p=2 -parallel=4 -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "📊 Checking backend coverage threshold (≥60%)..."
	@cd backend && COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
		COVERAGE_INT=$$(echo "$$COVERAGE" | cut -d. -f1); \
		echo "Current coverage: $${COVERAGE}%"; \
		if [ "$$COVERAGE_INT" -lt 60 ]; then \
			echo "❌ Coverage $${COVERAGE}% is below threshold 60%"; \
			exit 1; \
		else \
			echo "✅ Coverage $${COVERAGE}% meets threshold 60%"; \
		fi
```

Replace with:
```makefile
	@cd backend && go test ./... -race -p=2 -parallel=4 \
		-coverprofile=coverage.out -covermode=atomic \
		-coverpkg=./internal/...,./cmd/plaid_validation/validators/... \
		./...
	@echo ""
	@echo "📊 Checking backend coverage threshold (≥68%)..."
	@./scripts/check-coverage.sh backend/coverage.out 68
```

- [ ] **Step 3: Update `test-all` backend gate (lines 157-168) and add frontend threshold**

Find the backend gate in test-all:
```makefile
	@cd backend && go test ./... -v -race -p=2 -parallel=4 -coverprofile=coverage.out -covermode=atomic ./...
	@echo ""
	@echo "📊 Checking backend coverage threshold (≥60%)..."
	@cd backend && COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
		COVERAGE_INT=$$(echo "$$COVERAGE" | cut -d. -f1); \
		echo "Current coverage: $${COVERAGE}%"; \
		if [ "$$COVERAGE_INT" -lt 60 ]; then \
			echo "❌ Coverage $${COVERAGE}% is below threshold 60%"; \
			exit 1; \
		else \
			echo "✅ Coverage $${COVERAGE}% meets threshold 60%"; \
		fi
```

Replace with:
```makefile
	@cd backend && go test ./... -v -race -p=2 -parallel=4 \
		-coverprofile=coverage.out -covermode=atomic \
		-coverpkg=./internal/...,./cmd/plaid_validation/validators/... \
		./...
	@echo ""
	@echo "📊 Checking backend coverage threshold (≥68%)..."
	@./scripts/check-coverage.sh backend/coverage.out 68
```

Then find the lcov combine block in test-all (lines 176-189) and add a threshold call after it:

Find (the end of the lcov combine block):
```makefile
	else \
		echo "⚠️  lcov not installed — skipping coverage combine (install: brew install lcov)"; \
	fi
	@echo ""
	@echo "4️⃣  MCP Server Tests..."
```

Replace with:
```makefile
	else \
		echo "⚠️  lcov not installed — skipping coverage combine (install: brew install lcov)"; \
	fi
	@echo "📊 Checking frontend coverage threshold (≥74% lines)..."
	@if command -v lcov &> /dev/null; then \
		./scripts/check-frontend-coverage.sh apps/web 74; \
	else \
		echo "⚠️  lcov not installed — skipping frontend threshold check"; \
	fi
	@echo ""
	@echo "4️⃣  MCP Server Tests..."
```

- [ ] **Step 4: Verify Makefile targets work locally**

```bash
# Quick smoke-test: run just the coverage check portion against existing artifacts
./scripts/check-coverage.sh backend/coverage.out 68
./scripts/check-frontend-coverage.sh apps/web 60   # use 60 for the first pass while artifacts may be stale
```

- [ ] **Step 5: Commit**

```bash
git add Makefile
git commit -m "#1286: consolidate Makefile coverage gates into shared scripts, scope -coverpkg, add frontend threshold to test-all"
```

---

### Task 7: Documentation fixes

**Files:**
- Modify: `README.md` (line 5, line 251)
- Modify: `CLAUDE.md` (line 104)

The README CI badge points to a non-existent `ci-cd.yml` workflow; the Contributing section says ">80% required"; CLAUDE.md says ">60% backend, >80% overall". All three misrepresent reality.

- [ ] **Step 1: Fix the README CI badge (line 5)**

Find:
```markdown
[![CI/CD Pipeline](https://github.com/maxnussbaum/BoostedBudget/actions/workflows/ci-cd.yml/badge.svg)](https://github.com/maxnussbaum/BoostedBudget/actions/workflows/ci-cd.yml)
```

Replace with:
```markdown
[![Test Suite](https://github.com/maxnussbaum/BoostedBudget/actions/workflows/test.yml/badge.svg)](https://github.com/maxnussbaum/BoostedBudget/actions/workflows/test.yml)
```

- [ ] **Step 2: Fix the Contributing section coverage line (line 251)**

Find:
```markdown
3. **Test Coverage:** >80% required
```

Replace with:
```markdown
3. **Test Coverage:** Backend ≥68% statements (hard gate), Frontend ≥74% lines (hard gate). Aspirational goal: ≥80% on both sides (active campaign — issue #1286).
```

- [ ] **Step 3: Fix CLAUDE.md coverage target (line 104)**

Find:
```
- Target: >60% backend, >80% overall
```

Replace with:
```
- Enforced floors: ≥68% backend statements, ≥74% frontend lines (as of #1286). Aspirational target: ≥80% both sides (active campaign).
```

- [ ] **Step 4: Verify the badge URL is correct**

```bash
# Confirm the workflow file exists
ls .github/workflows/test.yml && echo "workflow file exists"
```

Expected: `workflow file exists`

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "#1286: fix README CI badge (ci-cd.yml → test.yml), reconcile coverage threshold docs with enforced floors"
```

---

### Task 8: Full local verification

**Files:** No new changes — just running the full test suite to confirm everything is consistent before opening the PR.

- [ ] **Step 1: Run `make test-ci`**

```bash
make test-ci
```

Expected: backend tests pass, backend coverage ≥68%, frontend tests pass, frontend coverage ≥74%. No `exit 1`.

- [ ] **Step 2: Verify script error paths**

```bash
# Force a backend failure
./scripts/check-coverage.sh backend/coverage.out 99
# Expected: exit 1 with red ❌ message

# Force a frontend failure
./scripts/check-frontend-coverage.sh apps/web 99
# Expected: exit 1 with red ❌ message
```

- [ ] **Step 3: Run the frontend typecheck and lint**

```bash
cd apps/web && bun run typecheck && bun run lint
```

Expected: no errors.

- [ ] **Step 4: Validate YAML**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/test.yml'))" && echo "test.yml OK"
```

---

### Task 9: Open the PR

- [ ] **Step 1: Push the branch**

```bash
git push -u origin feature/1286-coverage-infrastructure
```

- [ ] **Step 2: Create the PR**

Use `/commit-push-pr` or:

```bash
gh pr create \
  --title "#1286: Coverage infrastructure — scope denominator, consolidate gates, raise floors, fix docs" \
  --body "## Summary
- Scope backend coverage denominator via \`-coverpkg=./internal/...,./cmd/plaid_validation/validators/...\` (excludes pure entrypoints from the 0%-drag)
- Add \`scripts/check-frontend-coverage.sh\`: merges all lcov groups, filters \`packages/api-client/**\` (orval-generated), enforces Lines threshold with decimal-safe awk comparison
- Consolidate 4 duplicate inline backend gates (test.yml, test-fast, test-all, test-ci) into single \`scripts/check-coverage.sh\` call
- Raise floors: backend 60% → 68%, frontend Lines 63% → 74% (both reflect post-exclusion actuals)
- Fix CI lcov locale issue (\`LC_ALL=C LANG=C\`)
- Drop dead Functions/Branches soft gates (Bun LCOV emits no data for them)
- Fix README CI badge (pointed at non-existent ci-cd.yml → test.yml)
- Reconcile CLAUDE.md and README ">80% required" prose with real enforced floors

## Test plan
- [ ] \`make test-ci\` passes locally (backend ≥68%, frontend ≥74%)
- [ ] \`./scripts/check-coverage.sh backend/coverage.out 99\` exits 1 (error path)
- [ ] \`./scripts/check-frontend-coverage.sh apps/web 99\` exits 1 (error path)
- [ ] GitHub Actions \`Backend Tests\` and \`Frontend Tests\` jobs pass in CI

Closes #1286"
```

---

## Plans B, C, D (defined here for sequencing — written separately after Plan A lands)

**Plan B — Backend test campaign** *(start after Plan A PR merges and CI baseline is confirmed)*
Priority: `internal/auth/claims.go` → `internal/queue` + `internal/queue/river` + `internal/database` → `cmd/plaid_validation/validators` → `internal/repository` (critical path to 80%). Ratchet floor in steps: 68→72→76→80.

**Plan C — Frontend test campaign** *(can start in parallel with Plan B)*
Priority: `DashboardGrid.tsx` → `WidgetCatalog.tsx` → `ErrorBoundary` → low-coverage hooks → `PlaidLinkButton` / `CookieConsent` / `SubscriptionManagementPage` → `src/store` → thin features. Ratchet floor in steps: 74→77→80.

**Plan D — Finalize gates** *(after B and C both land)*
Set both floors to 80% (or highest defensible level based on actual measurements). Confirm dead Functions/Branches gates are removed. Update CLAUDE.md to reflect final enforced floors.
