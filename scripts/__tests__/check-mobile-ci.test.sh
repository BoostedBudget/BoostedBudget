#!/usr/bin/env bash
# Falsification suite for the mobile CI and Dependabot policy guard (#1654).
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GUARD="$REPO_ROOT/scripts/check-mobile-ci.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0
fail=0
ok() { echo "  PASS: $1"; pass=$((pass + 1)); }
bad() { echo "  FAIL: $1"; fail=$((fail + 1)); }

copy_fixture() {
  local dest="$1"
  mkdir -p "$dest/.github/workflows" "$dest/apps/mobile" "$dest/scripts/__tests__"
  cp "$REPO_ROOT/.github/workflows/test.yml" "$dest/.github/workflows/test.yml"
  cp "$REPO_ROOT/.github/dependabot.yml" "$dest/.github/dependabot.yml"
  cp "$REPO_ROOT/apps/mobile/package.json" "$dest/apps/mobile/package.json"
  touch "$dest/bun.lock"
}

mutate() {
  python3 - "$1" "$2" "$3" <<'PY'
import pathlib, sys
path, old, new = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
text = path.read_text()
changed = text.replace(old, new, 1)
assert changed != text, f"mutation did not apply: {old!r}"
path.write_text(changed)
PY
}

guard_passes() { python3 "$GUARD" "$1" >/dev/null 2>&1; }

echo "1/11: the real repository configuration passes"
if guard_passes "$REPO_ROOT"; then ok "real configuration accepted"; else bad "real configuration rejected"; fi

echo "2/11: a wrong paths-filter output step id is rejected"
copy_fixture "$TMP/output"
mutate "$TMP/output/.github/workflows/test.yml" 'steps.filter.outputs.mobile' 'steps.mobile_filter.outputs.mobile'
if guard_passes "$TMP/output"; then bad "wrong output step id accepted"; else ok "wrong output step id rejected"; fi

echo "3/11: a missing OpenAPI positive trigger is rejected"
copy_fixture "$TMP/openapi"
mutate "$TMP/openapi/.github/workflows/test.yml" $'            mobile:\n              - \'apps/mobile/**\'\n              - \'packages/api-client/**\'\n              - \'packages/design-tokens/**\'\n              - \'api/openapi.yaml\'' $'            mobile:\n              - \'apps/mobile/**\'\n              - \'packages/api-client/**\'\n              - \'packages/design-tokens/**\'\n              - \'api/not-the-contract.yaml\''
if guard_passes "$TMP/openapi"; then bad "missing OpenAPI trigger accepted"; else ok "missing OpenAPI trigger rejected"; fi

echo "4/11: a backend-wide trigger is rejected"
copy_fixture "$TMP/backend"
mutate "$TMP/backend/.github/workflows/test.yml" "              - 'apps/mobile/**'" "              - 'apps/mobile/**'\n              - 'backend/**'"
if guard_passes "$TMP/backend"; then bad "backend-only changes over-trigger mobile"; else ok "backend over-trigger rejected"; fi

echo "5/11: an unrelated packages glob is rejected"
copy_fixture "$TMP/packages"
mutate "$TMP/packages/.github/workflows/test.yml" "              - 'packages/api-client/**'" "              - 'packages/**'"
if guard_passes "$TMP/packages"; then bad "packages/** over-trigger accepted"; else ok "packages/** over-trigger rejected"; fi

echo "6/11: wrong mobile needs/if wiring is rejected"
copy_fixture "$TMP/needs"
mutate "$TMP/needs/.github/workflows/test.yml" $'  mobile-test:\n    name: Mobile Build Scaffold\n    needs: detect-changes' $'  mobile-test:\n    name: Mobile Build Scaffold\n    needs: frontend-test'
if guard_passes "$TMP/needs"; then bad "wrong needs/if accepted"; else ok "wrong needs/if rejected"; fi

echo "7/11: mobile runner drift is rejected independently"
copy_fixture "$TMP/runner"
mutate "$TMP/runner/.github/workflows/test.yml" $'  mobile-test:\n    name: Mobile Build Scaffold\n    needs: detect-changes\n    if: needs.detect-changes.outputs.mobile == \'true\'\n    runs-on: ubicloud-standard-2' $'  mobile-test:\n    name: Mobile Build Scaffold\n    needs: detect-changes\n    if: needs.detect-changes.outputs.mobile == \'true\'\n    runs-on: [self-hosted, macOS, mac-mini]'
if guard_passes "$TMP/runner"; then bad "mobile runner drift accepted"; else ok "mobile runner drift rejected"; fi

echo "8/11: a missing platform export is rejected independently"
copy_fixture "$TMP/commands"
mutate "$TMP/commands/.github/workflows/test.yml" 'bun run --cwd apps/mobile export:android' 'echo skipped-android-export'
if guard_passes "$TMP/commands"; then bad "missing Android export accepted"; else ok "missing Android export rejected"; fi

echo "9/11: disabling the otherwise-ungated guard host is rejected"
copy_fixture "$TMP/gated"
mutate "$TMP/gated/.github/workflows/test.yml" '    if: always() && !cancelled()' '    if: false'
if guard_passes "$TMP/gated"; then bad "disabled guard host accepted"; else ok "disabled guard host rejected"; fi

echo "10/11: breaking the Expo compatibility group is rejected"
copy_fixture "$TMP/group"
mutate "$TMP/group/.github/dependabot.yml" $'          - "react-dom"\n          - "react-native*"' $'          - "react-dom"\n          - "react-native-web"'
if guard_passes "$TMP/group"; then bad "incomplete compatibility group accepted"; else ok "incomplete compatibility group rejected"; fi

echo "11/11: a second Bun updater or mobile lockfile is rejected"
copy_fixture "$TMP/lock"
cp "$TMP/lock/bun.lock" "$TMP/lock/apps/mobile/bun.lock"
python3 - "$TMP/lock/.github/dependabot.yml" <<'PY'
import pathlib
p = pathlib.Path(__import__('sys').argv[1])
t = p.read_text()
needle = 'updates:\n'
extra = '''updates:\n  - package-ecosystem: "bun"\n    directory: "/apps/mobile"\n    schedule:\n      interval: "weekly"\n'''
p.write_text(t.replace(needle, extra, 1))
PY
if guard_passes "$TMP/lock"; then bad "competing updater/mobile lockfile accepted"; else ok "competing updater/mobile lockfile rejected"; fi

echo "check-mobile-ci.test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
