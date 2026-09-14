#!/usr/bin/env bash
# Falsification suite for the mobile CI and Dependabot policy guard (#1654).
# Bash 3.2 compatible: this runs locally on macOS and in the Linux e2e-test job.
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
GUARD="$REPO_ROOT/scripts/check-mobile-ci.py"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

TOTAL=168
case_no=0
pass=0
fail=0
ok() { echo "  PASS: $1"; pass=$((pass + 1)); }
bad() { echo "  FAIL: $1"; fail=$((fail + 1)); }
announce() {
  case_no=$((case_no + 1))
  echo "$case_no/$TOTAL: $1"
}

copy_fixture() {
  local dest="$1"
  mkdir -p "$dest/.github/workflows" "$dest/apps/mobile" "$dest/apps/web" \
    "$dest/packages/api-client" "$dest/packages/design-tokens"
  cp "$REPO_ROOT/.github/workflows/test.yml" "$dest/.github/workflows/test.yml"
  cp "$REPO_ROOT/.github/workflows/warm-ci-cache.yml" "$dest/.github/workflows/warm-ci-cache.yml"
  cp "$REPO_ROOT/.github/dependabot.yml" "$dest/.github/dependabot.yml"
  cp "$REPO_ROOT/apps/mobile/package.json" "$dest/apps/mobile/package.json"
  cp "$REPO_ROOT/apps/web/package.json" "$dest/apps/web/package.json"
  cp "$REPO_ROOT/packages/api-client/package.json" "$dest/packages/api-client/package.json"
  cp "$REPO_ROOT/packages/design-tokens/package.json" "$dest/packages/design-tokens/package.json"
  cp "$REPO_ROOT/package.json" "$dest/package.json"
  touch "$dest/bun.lock"
}

mutate_workspace_config() {
  python3 - "$1" "$2" <<'PY'
import json, pathlib, sys
path, action = pathlib.Path(sys.argv[1]) / 'package.json', sys.argv[2]
doc = json.loads(path.read_text())
if action == 'missing':
    doc.pop('workspaces', None)
elif action == 'malformed':
    doc['workspaces'] = 'apps/*'
elif action == 'unsafe-deep':
    workspace = path.parent / 'vendor/cache/example'
    workspace.mkdir(parents=True)
    (workspace / 'package.json').write_text('{}\n')
    doc['workspaces'] = ['vendor/cache/*']
elif action == 'symlink-parent':
    apps = path.parent / 'apps'
    outside = path.parent / 'outside-apps'
    apps.rename(outside)
    apps.symlink_to(outside, target_is_directory=True)
elif action == 'symlink-child':
    outside = path.parent.parent / 'outside-workspace'
    outside.mkdir(exist_ok=True)
    (outside / 'package.json').write_text('{}\n')
    (path.parent / 'apps/link').symlink_to(outside, target_is_directory=True)
else:
    raise AssertionError(action)
path.write_text(json.dumps(doc, indent=2) + '\n')
PY
}

mutate_playwright_resolution() {
  python3 - "$1" "$2" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
action = sys.argv[2]
text = path.read_text()
portable = "require(require.resolve('@playwright/test/package.json', { paths: ['./apps/web'] })).version"
assert text.count(portable) == 1, (path, text.count(portable))
broken = "require('./apps/web/node_modules/@playwright/test/package.json').version"
if action == 'broken':
    text = text.replace(portable, broken)
elif action == 'comment':
    assignment = f'version="$(node -p "{portable}")"'
    replacement = f'# {portable}\n          version="$(node -p "{broken}")"'
    assert text.count(assignment) == 1, (path, text.count(assignment))
    text = text.replace(assignment, replacement)
elif action == 'disabled':
    import yaml
    doc = yaml.safe_load(text)
    matches = [
        step
        for job in doc['jobs'].values()
        for step in job.get('steps', [])
        if step.get('name') == 'Resolve Playwright version'
    ]
    assert len(matches) == 1, (path, len(matches))
    matches[0]['if'] = False
    text = yaml.safe_dump(doc, sort_keys=False)
else:
    raise AssertionError(action)
path.write_text(text)
PY
}

# Mutate exactly one workflow invariant through the parsed structure. Re-serializing the fixture
# is intentional: the production guard must depend on YAML semantics, not comments or formatting.
mutate_workflow() {
  python3 - "$1" "$2" "$3" <<'PY'
import pathlib, sys, yaml
path, action, value = pathlib.Path(sys.argv[1]), sys.argv[2], sys.argv[3]
doc = yaml.safe_load(path.read_text())
jobs = doc['jobs']
if action == 'output-id':
    jobs['detect-changes']['outputs']['mobile'] = '${{ steps.mobile_filter.outputs.mobile }}'
elif action in ('remove-path', 'add-path'):
    step = next(s for s in jobs['detect-changes']['steps'] if s.get('id') == 'filter')
    filters = yaml.safe_load(step['with']['filters'])
    if action == 'remove-path':
        assert value in filters['mobile'], value
        filters['mobile'].remove(value)
    else:
        assert value not in filters['mobile'], value
        filters['mobile'].append(value)
    step['with']['filters'] = yaml.safe_dump(filters, sort_keys=False)
elif action == 'mobile-field':
    field, replacement = value.split('=', 1)
    jobs['mobile-test'][field] = replacement
elif action == 'node-version':
    step = next(s for s in jobs['mobile-test']['steps'] if str(s.get('uses', '')).startswith('actions/setup-node@'))
    step['with']['node-version'] = value
elif action == 'bun-version':
    step = next(s for s in jobs['mobile-test']['steps'] if str(s.get('uses', '')).startswith('oven-sh/setup-bun@'))
    step['with']['bun-version'] = value
elif action == 'replace-command':
    old, new = value.split('|', 1)
    matches = [s for s in jobs['mobile-test']['steps'] if old in str(s.get('run', ''))]
    assert len(matches) == 1, (old, len(matches))
    matches[0]['run'] = str(matches[0]['run']).replace(old, new)
elif action in ('echo-command', 'comment-command', 'dead-command', 'disable-command'):
    matches = [s for s in jobs['mobile-test']['steps'] if value in str(s.get('run', ''))]
    assert len(matches) == 1, (value, len(matches))
    if action == 'echo-command':
        matches[0]['run'] = 'echo ' + value
    elif action == 'comment-command':
        matches[0]['run'] = '# ' + value + '\necho no-op'
    elif action == 'dead-command':
        matches[0]['run'] = 'if false; then ' + value + '; fi'
    else:
        matches[0]['if'] = False
elif action == 'doctor-working-directory':
    matches = [s for s in jobs['mobile-test']['steps'] if 'doctor' in str(s.get('run', ''))]
    assert len(matches) == 1, len(matches)
    matches[0]['working-directory'] = value
elif action == 'doctor-unpinned':
    matches = [s for s in jobs['mobile-test']['steps'] if 'doctor' in str(s.get('run', ''))]
    assert len(matches) == 1, len(matches)
    matches[0]['run'] = 'bunx expo-doctor'
elif action == 'float-action':
    matches = [s for s in jobs['mobile-test']['steps'] if str(s.get('uses', '')).startswith(value + '@')]
    assert len(matches) == 1, (value, len(matches))
    matches[0]['uses'] = value + '@v4'
elif action == 'disable-action':
    matches = [s for s in jobs['mobile-test']['steps'] if str(s.get('uses', '')).startswith(value + '@')]
    assert len(matches) == 1, (value, len(matches))
    matches[0]['if'] = False
elif action == 'host-needs':
    jobs['e2e-test']['needs'] = value
elif action == 'host-if':
    jobs['e2e-test']['if'] = False
else:
    raise AssertionError(action)
path.write_text(yaml.safe_dump(doc, sort_keys=False))
PY
}

mutate_mobile_package() {
  python3 - "$1" "$2" <<'PY'
import json, pathlib, sys
path, action = pathlib.Path(sys.argv[1]), sys.argv[2]
doc = json.loads(path.read_text())
if action == 'remove-doctor':
    doc['devDependencies'].pop('expo-doctor', None)
elif action == 'range-doctor':
    doc['devDependencies']['expo-doctor'] = '^1.20.4'
else:
    raise AssertionError(action)
path.write_text(json.dumps(doc, indent=2) + '\n')
PY
}

mutate_dependency_policy() {
  python3 - "$1" "$2" <<'PY'
import json, pathlib, sys
root, action = pathlib.Path(sys.argv[1]), sys.argv[2]
paths = {
    'root': root / 'package.json',
    'web': root / 'apps/web/package.json',
    'mobile': root / 'apps/mobile/package.json',
    'api-client': root / 'packages/api-client/package.json',
}

def load(name):
    path = paths[name]
    return path, json.loads(path.read_text())

def save(path, doc):
    path.write_text(json.dumps(doc, indent=2) + '\n')

if action == 'allowed-react-patch':
    for name, section in (('root', 'dependencies'), ('web', 'dependencies'), ('mobile', 'dependencies'), ('api-client', 'devDependencies')):
        path, doc = load(name)
        doc[section]['react'] = '19.2.4'
        doc[section]['react-dom'] = '19.2.4'
        save(path, doc)
elif action in ('coordinated-react-range', 'coordinated-react-minor'):
    value = '^19.2.3' if action == 'coordinated-react-range' else '19.3.0'
    for name, section in (('root', 'dependencies'), ('web', 'dependencies'), ('mobile', 'dependencies'), ('api-client', 'devDependencies')):
        path, doc = load(name)
        doc[section]['react'] = value
        doc[section]['react-dom'] = value
        save(path, doc)
elif action.startswith('set:'):
    _, name, section, dependency, value = action.split(':', 4)
    path, doc = load(name)
    doc[section][dependency] = value
    save(path, doc)
elif action.startswith('override:'):
    _, dependency, value = action.split(':', 2)
    path, doc = load('root')
    doc.setdefault('overrides', {})[dependency] = value
    save(path, doc)
elif action == 'qualified-override':
    path, doc = load('root')
    doc.setdefault('overrides', {})['lightningcss@*'] = '1.30.2'
    save(path, doc)
elif action == 'nested-override':
    path, doc = load('root')
    doc.setdefault('overrides', {})['some-package'] = {'react': '19.2.3'}
    save(path, doc)
else:
    raise AssertionError(action)
PY
}

mutate_dependabot() {
  python3 - "$1" "$2" <<'PY'
import pathlib, sys, yaml
path, action = pathlib.Path(sys.argv[1]), sys.argv[2]
doc = yaml.safe_load(path.read_text())
bun = next(u for u in doc['updates'] if u.get('package-ecosystem') == 'bun')
groups = bun['groups']
if action == 'second-updater':
    doc['updates'].insert(0, {'package-ecosystem': 'bun', 'directory': '/apps/mobile', 'schedule': {'interval': 'weekly'}})
elif action == 'root-updater-wrong-directory':
    bun['directory'] = '/apps/mobile'
elif action == 'group-last':
    group = groups.pop('expo-react-native-compatibility')
    groups['expo-react-native-compatibility'] = group
elif action == 'group-major-only':
    groups['expo-react-native-compatibility']['update-types'] = ['major']
elif action == 'remove-required-pattern':
    groups['expo-react-native-compatibility']['patterns'].remove('nativewind')
elif action == 'exclude-doctor-from-group':
    patterns = groups['expo-react-native-compatibility']['patterns']
    patterns.remove('expo-*')
    patterns.extend(['expo-constants', 'expo-linking', 'expo-router', 'expo-status-bar'])
elif action == 'generic-overlap':
    groups['frontend-framework']['exclude-patterns'].remove('react-dom')
elif action == 'remove-expo-major-ignore':
    bun['ignore'] = [i for i in bun['ignore'] if i.get('dependency-name') != 'expo*']
elif action == 'remove-react-native-major-ignore':
    for entry in bun['ignore']:
        if entry.get('dependency-name') in ('react-native*', 'react-native'):
            entry['update-types'] = [
                value for value in entry.get('update-types', [])
                if value != 'version-update:semver-major'
            ]
elif action == 'remove-nativewind-major-ignore':
    bun['ignore'] = [i for i in bun['ignore'] if i.get('dependency-name') != 'nativewind']
elif action == 'narrow-expo-major-ignore':
    next(i for i in bun['ignore'] if i.get('dependency-name') == 'expo*')['dependency-name'] = 'expo'
elif action == 'remove-react-native-family-ignore':
    bun['ignore'] = [i for i in bun['ignore'] if i.get('dependency-name') != 'react-native*']
elif action == 'add-react-family-patch-ignore':
    bun['ignore'].append({
        'dependency-name': 'react*',
        'update-types': ['version-update:semver-patch'],
    })
elif action == 'add-react-native-family-patch-ignore':
    entry = next(i for i in bun['ignore'] if i.get('dependency-name') == 'react-native*')
    entry['update-types'].append('version-update:semver-patch')
elif action == 'add-react-patch-version-ignore':
    entry = next(i for i in bun['ignore'] if i.get('dependency-name') == 'react')
    entry['versions'] = ['19.2.4']
elif action == 'add-unconditional-react-family-ignore':
    bun['ignore'].append({'dependency-name': 'react*'})
elif action.startswith('remove-ignore-type:'):
    _, dependency, update_type = action.split(':', 2)
    entry = next((i for i in bun['ignore'] if i.get('dependency-name') == dependency), None)
    if entry is not None:
        entry['update-types'] = [value for value in entry.get('update-types', []) if value != update_type]
else:
    raise AssertionError(action)
path.write_text(yaml.safe_dump(doc, sort_keys=False))
PY
}

guard_passes() { python3 "$GUARD" "$1" >/dev/null 2>&1; }

guard_rejects() {
  local dir="$1" expected="$2" diagnostics="$TMP/guard-stderr"
  if python3 "$GUARD" "$dir" >/dev/null 2>"$diagnostics"; then
    return 1
  fi
  ! grep -q 'Traceback' "$diagnostics" &&
    grep -q '^  - ' "$diagnostics" &&
    grep -Eiq -- "$expected" "$diagnostics"
}

reject_workflow() {
  local label="$1" action="$2" value="$3" dir="$TMP/case-$((case_no + 1))"
  announce "$label"
  copy_fixture "$dir"
  mutate_workflow "$dir/.github/workflows/test.yml" "$action" "$value"
  if guard_rejects "$dir" 'mobile-test|mobile trigger|mobile filter|Playwright|e2e-test|paths-filter|detect-changes|action'; then
    ok "rejected with intended diagnostic"
  else
    bad "accepted, crashed, or returned an unrelated diagnostic: $label"
  fi
}

reject_dependabot() {
  local label="$1" action="$2" dir="$TMP/case-$((case_no + 1))"
  announce "$label"
  copy_fixture "$dir"
  mutate_dependabot "$dir/.github/dependabot.yml" "$action"
  if guard_rejects "$dir" 'Dependabot|compatibility group'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated diagnostic: $label"; fi
}

reject_mobile_package() {
  local label="$1" action="$2" dir="$TMP/case-$((case_no + 1))"
  announce "$label"
  copy_fixture "$dir"
  mutate_mobile_package "$dir/apps/mobile/package.json" "$action"
  if guard_rejects "$dir" 'expo-doctor'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated diagnostic: $label"; fi
}

reject_workspace_lockfile() {
  local label="$1" relative_path="$2" dir="$TMP/case-$((case_no + 1))"
  announce "$label"
  copy_fixture "$dir"
  touch "$dir/$relative_path"
  if guard_rejects "$dir" "$relative_path"; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated diagnostic: $label"; fi
}

reject_workspace_config() {
  local label="$1" action="$2" dir="$TMP/case-$((case_no + 1))"
  announce "$label"
  copy_fixture "$dir"
  mutate_workspace_config "$dir" "$action"
  if guard_rejects "$dir" 'workspace'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated diagnostic: $label"; fi
}

reject_dependency_policy() {
  local label="$1" action="$2" dir="$TMP/case-$((case_no + 1))" expected='React|overrides'
  announce "$label"
  copy_fixture "$dir"
  mutate_dependency_policy "$dir" "$action"
  case "$action" in
    set:*)
      local unused package section dependency value
      IFS=: read -r unused package section dependency value <<EOF
$action
EOF
      expected="$dependency"
      ;;
    override:*) expected="${action#override:}"; expected="${expected%%:*}" ;;
  esac
  if guard_rejects "$dir" "$expected"; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated diagnostic: $label"; fi
}

accept_dependency_policy() {
  local label="$1" action="$2" dir="$TMP/case-$((case_no + 1))"
  announce "$label"
  copy_fixture "$dir"
  mutate_dependency_policy "$dir" "$action"
  if guard_passes "$dir"; then ok "accepted"; else bad "rejected: $label"; fi
}

reject_synthetic_react() {
  local label="$1" section="$2" dependency="$3" version="$4" dir="$TMP/case-$((case_no + 1))"
  announce "$label"
  copy_fixture "$dir"
  mkdir -p "$dir/apps/synthetic"
  python3 - "$dir/apps/synthetic/package.json" "$section" "$dependency" "$version" <<'PY'
import json, pathlib, sys
pathlib.Path(sys.argv[1]).write_text(json.dumps({
    'name': '@boostedbudget/synthetic',
    'private': True,
    sys.argv[2]: {sys.argv[3]: sys.argv[4]},
}, indent=2) + '\n')
PY
  if guard_rejects "$dir" "$dependency"; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated diagnostic: $label"; fi
}

announce "the real repository configuration passes"
if guard_passes "$REPO_ROOT"; then ok "real configuration accepted"; else bad "real configuration rejected"; fi

reject_workflow "wrong paths-filter output step id" "output-id" "unused"

# Every positive input is removed independently. This catches a guard whose required-path set
# accidentally protects one dependency while merely appearing to cover the whole matrix.
reject_workflow "missing apps/mobile/** trigger" "remove-path" "apps/mobile/**"
reject_workflow "missing API-client trigger" "remove-path" "packages/api-client/**"
reject_workflow "missing design-token trigger" "remove-path" "packages/design-tokens/**"
reject_workflow "missing OpenAPI trigger" "remove-path" "api/openapi.yaml"
reject_workflow "missing root package trigger" "remove-path" "package.json"
reject_workflow "missing root lockfile trigger" "remove-path" "bun.lock"
reject_workflow "missing bunfig trigger" "remove-path" "bunfig.toml"
reject_workflow "missing Turbo graph trigger" "remove-path" "turbo.json"
reject_workflow "missing root TypeScript config trigger" "remove-path" "tsconfig.json"
reject_workflow "missing test-workflow self-trigger" "remove-path" ".github/workflows/test.yml"
reject_workflow "missing Dependabot-policy trigger" "remove-path" ".github/dependabot.yml"
reject_workflow "missing production-guard trigger" "remove-path" "scripts/check-mobile-ci.py"
reject_workflow "missing guard-suite trigger" "remove-path" "scripts/__tests__/check-mobile-ci.test.sh"

# Each negative row gets its own narrow over-trigger. None relies on packages/** or backend/** to
# make several assertions pass for the same reason.
reject_workflow "backend service changes over-trigger mobile" "add-path" "backend/internal/**"
reject_workflow "backend config-source changes over-trigger mobile" "add-path" "backend/config/**"
reject_workflow "migrations over-trigger mobile" "add-path" "backend/migrations/**"
reject_workflow "desktop web changes over-trigger mobile" "add-path" "apps/web/**"
reject_workflow "docs changes over-trigger mobile" "add-path" "docs/**"
reject_workflow "shared-types changes over-trigger mobile" "add-path" "packages/shared-types/**"
reject_workflow "product config changes over-trigger mobile" "add-path" "config/**"
reject_workflow "root README changes over-trigger mobile" "add-path" "README.md"
reject_workflow "broad API changes over-trigger mobile" "add-path" "api/**"
reject_workflow "broad script changes over-trigger mobile" "add-path" "scripts/**"
reject_workflow "infrastructure changes over-trigger mobile" "add-path" "infrastructure/**"

reject_workflow "mobile job has wrong needs" "mobile-field" "needs=frontend-test"
reject_workflow "mobile job has wrong if expression" "mobile-field" "if=needs.detect-changes.outputs.frontend == 'true'"
reject_workflow "mobile job leaves Ubicloud" "mobile-field" "runs-on=self-hosted"
reject_workflow "mobile job lowers Node below 22.13" "node-version" "22.12.x"
reject_workflow "mobile job drifts from Bun 1.3.14" "bun-version" "latest"
reject_workflow "mobile install is not frozen" "replace-command" "bun install --frozen-lockfile|bun install"
reject_workflow "design-token generated check is missing" "replace-command" "bun run --filter @boostedbudget/design-tokens check|echo skipped-token-check"
reject_workflow "design-token tests are missing" "replace-command" "bun run --filter @boostedbudget/design-tokens test|echo skipped-token-tests"
reject_workflow "API-client tests are missing" "replace-command" "bun run --filter @boostedbudget/api-client test|echo skipped-api-client-tests"
reject_workflow "mobile lint is missing" "replace-command" "bun run --cwd apps/mobile lint|echo skipped-lint"
reject_workflow "mobile typecheck is missing" "replace-command" "bun run --cwd apps/mobile typecheck|echo skipped-typecheck"
reject_workflow "Jest line floor drops below 82" "replace-command" '"global":{"lines":82}|"global":{"lines":81}'
reject_workflow "Expo Doctor is missing" "replace-command" "bun run doctor|echo skipped-expo-doctor"
reject_workflow "web export is missing" "replace-command" "bun run --cwd apps/mobile export:web|echo skipped-web-export"
reject_workflow "iOS export is missing" "replace-command" "bun run --cwd apps/mobile export:ios|echo skipped-ios-export"
reject_workflow "Android export is missing" "replace-command" "bun run --cwd apps/mobile export:android|echo skipped-android-export"

reject_playwright_resolution() {
  announce "$1"
  local fixture="$TMP/playwright-$case_no"
  copy_fixture "$fixture"
  mutate_playwright_resolution "$fixture/$2" "$3"
  if guard_rejects "$fixture" 'Playwright'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated Playwright diagnostic"; fi
}

reject_playwright_resolution "E2E Playwright resolution assumes nested node_modules" ".github/workflows/test.yml" "broken"
reject_playwright_resolution "cache warmer Playwright resolution assumes nested node_modules" ".github/workflows/warm-ci-cache.yml" "broken"
reject_playwright_resolution "E2E portable resolver survives only in a comment" ".github/workflows/test.yml" "comment"
reject_playwright_resolution "cache warmer portable resolver survives only in a comment" ".github/workflows/warm-ci-cache.yml" "comment"
reject_playwright_resolution "E2E Playwright resolver is disabled" ".github/workflows/test.yml" "disabled"
reject_playwright_resolution "cache warmer Playwright resolver is disabled" ".github/workflows/warm-ci-cache.yml" "disabled"

# These retain the required command as inert text. A global substring scan accepts every one.
reject_workflow "echo masquerades as mobile lint" "echo-command" "bun run --cwd apps/mobile lint"
reject_workflow "comment masquerades as mobile typecheck" "comment-command" "bun run --cwd apps/mobile typecheck"
reject_workflow "dead shell branch masquerades as web export" "dead-command" "bun run --cwd apps/mobile export:web"
reject_workflow "required design-token step is disabled" "disable-command" "bun run --filter @boostedbudget/design-tokens check"
reject_workflow "Expo Doctor runs from the wrong working directory" "doctor-working-directory" "."
reject_workflow "checkout action floats on a tag" "float-action" "actions/checkout"
reject_workflow "setup-node action floats on a tag" "float-action" "actions/setup-node"
reject_workflow "setup-bun action floats on a tag" "float-action" "oven-sh/setup-bun"
reject_workflow "checkout action is disabled" "disable-action" "actions/checkout"
reject_workflow "setup-node action is disabled" "disable-action" "actions/setup-node"
reject_workflow "setup-bun action is disabled" "disable-action" "oven-sh/setup-bun"

reject_mobile_package "Expo Doctor is absent from the mobile manifest" "remove-doctor"
reject_mobile_package "Expo Doctor uses a non-exact version range" "range-doctor"
reject_workflow "CI invokes unpinned Expo Doctor through bunx" "doctor-unpinned" "unused"

reject_workflow "guard host gains paths-filter needs" "host-needs" "detect-changes"
reject_workflow "guard host is disabled" "host-if" "unused"

reject_dependabot "a second Bun updater competes with the root updater" "second-updater"
reject_dependabot "the sole Bun updater moves away from the repository root" "root-updater-wrong-directory"

announce "a workspace-local Bun lockfile violates single-lockfile policy"
lock_dir="$TMP/case-$case_no"
copy_fixture "$lock_dir"
touch "$lock_dir/apps/mobile/bun.lock"
if guard_rejects "$lock_dir" 'apps/mobile/bun.lock'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated lockfile diagnostic"; fi

reject_workspace_lockfile "apps/web cannot own a Bun lockfile" "apps/web/bun.lock"
reject_workspace_lockfile "API-client cannot own a Bun lockfile" "packages/api-client/bun.lock"
reject_workspace_lockfile "design tokens cannot own a Bun lockfile" "packages/design-tokens/bun.lock"
reject_workspace_lockfile "workspace npm lockfiles are forbidden" "apps/web/package-lock.json"
reject_workspace_lockfile "workspace Yarn lockfiles are forbidden" "packages/api-client/yarn.lock"
reject_workspace_lockfile "workspace pnpm lockfiles are forbidden" "packages/design-tokens/pnpm-lock.yaml"
reject_workspace_lockfile "workspace binary Bun lockfiles are forbidden" "apps/mobile/bun.lockb"
reject_workspace_lockfile "root lockfiles other than bun.lock are forbidden" "package-lock.json"
reject_workspace_lockfile "workspace npm shrinkwrap files are forbidden" "apps/web/npm-shrinkwrap.json"
reject_workspace_lockfile "root npm shrinkwrap files are forbidden" "npm-shrinkwrap.json"

announce "a synthetic workspace discovered from apps/* cannot own a lockfile"
synthetic_dir="$TMP/case-$case_no"
copy_fixture "$synthetic_dir"
mkdir -p "$synthetic_dir/apps/synthetic"
cp "$REPO_ROOT/packages/design-tokens/package.json" "$synthetic_dir/apps/synthetic/package.json"
touch "$synthetic_dir/apps/synthetic/bun.lock"
if guard_rejects "$synthetic_dir" 'apps/synthetic/bun.lock'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated lockfile diagnostic"; fi

announce "a dangling workspace lockfile symlink is still forbidden"
dangling_lock_dir="$TMP/case-$case_no"
copy_fixture "$dangling_lock_dir"
python3 - "$dangling_lock_dir/apps/web/bun.lock" <<'PY'
import pathlib, sys
pathlib.Path(sys.argv[1]).symlink_to('missing-lock-target')
PY
if guard_rejects "$dangling_lock_dir" 'apps/web/bun.lock'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated lockfile diagnostic"; fi

announce "root bun.lock must be a regular in-repository file"
root_symlink_dir="$TMP/case-$case_no"
copy_fixture "$root_symlink_dir"
python3 - "$root_symlink_dir" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
(root / 'bun.lock').unlink()
outside = root.parent / 'external-bun.lock'
outside.touch()
(root / 'bun.lock').symlink_to(outside)
PY
if guard_rejects "$root_symlink_dir" 'root bun.lock'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated root-lock diagnostic"; fi

announce "nested node_modules lockfiles are not workspace lockfiles"
vendored_lock_dir="$TMP/case-$case_no"
copy_fixture "$vendored_lock_dir"
mkdir -p "$vendored_lock_dir/apps/web/node_modules/vendor"
touch "$vendored_lock_dir/apps/web/node_modules/vendor/bun.lock"
if guard_passes "$vendored_lock_dir"; then ok "ignored"; else bad "unrelated vendored lockfile rejected"; fi

announce "nested non-vendored lockfiles below apps are forbidden"
nested_lock_dir="$TMP/case-$case_no"
copy_fixture "$nested_lock_dir"
mkdir -p "$nested_lock_dir/apps/web/tooling/cache"
touch "$nested_lock_dir/apps/web/tooling/cache/package-lock.json"
if guard_rejects "$nested_lock_dir" 'apps/web/tooling/cache/package-lock.json'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated nested-lock diagnostic"; fi

announce "nested lockfile symlinks below packages are forbidden"
nested_symlink_dir="$TMP/case-$case_no"
copy_fixture "$nested_symlink_dir"
mkdir -p "$nested_symlink_dir/packages/api-client/tooling"
python3 - "$nested_symlink_dir/packages/api-client/tooling/yarn.lock" <<'PY'
import pathlib, sys
pathlib.Path(sys.argv[1]).symlink_to('missing-lock-target')
PY
if guard_rejects "$nested_symlink_dir" 'packages/api-client/tooling/yarn.lock'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated nested-lock diagnostic"; fi

announce "non-vendored symlink directories below apps are rejected"
symlink_directory_dir="$TMP/case-$case_no"
copy_fixture "$symlink_directory_dir"
external_directory="$TMP/external-directory-$case_no"
mkdir -p "$external_directory"
touch "$external_directory/bun.lock"
ln -s "$external_directory" "$symlink_directory_dir/apps/web/linked-tooling"
if guard_rejects "$symlink_directory_dir" 'apps/web/linked-tooling'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated symlink-directory diagnostic"; fi

announce "a symlink named node_modules remains outside lockfile traversal"
symlink_node_modules_dir="$TMP/case-$case_no"
copy_fixture "$symlink_node_modules_dir"
external_modules="$TMP/external-node-modules-$case_no"
mkdir -p "$external_modules/vendor"
touch "$external_modules/vendor/bun.lock"
ln -s "$external_modules" "$symlink_node_modules_dir/apps/web/node_modules"
if guard_passes "$symlink_node_modules_dir"; then ok "ignored"; else bad "symlinked node_modules lockfile was not ignored"; fi

reject_workspace_config "root package must declare workspaces" "missing"
reject_workspace_config "root workspaces must be a list of safe patterns" "malformed"
reject_workspace_config "deep workspace globs cannot enumerate unrelated trees" "unsafe-deep"
reject_workspace_config "workspace discovery cannot traverse a symlinked apps parent" "symlink-parent"
reject_workspace_config "workspace discovery rejects a symlinked direct child" "symlink-child"

announce "the root bun.lock is required"
root_lock_dir="$TMP/case-$case_no"
copy_fixture "$root_lock_dir"
python3 - "$root_lock_dir/bun.lock" <<'PY'
import pathlib, sys
pathlib.Path(sys.argv[1]).unlink()
PY
if guard_rejects "$root_lock_dir" 'root bun.lock'; then ok "rejected with intended diagnostic"; else bad "accepted, crashed, or returned an unrelated root-lock diagnostic"; fi

reject_dependabot "Expo compatibility group is not first" "group-last"
reject_dependabot "Expo compatibility group loses minor/patch update types" "group-major-only"
reject_dependabot "Expo compatibility group drops a required runtime pattern" "remove-required-pattern"
reject_dependabot "Expo compatibility group excludes Expo Doctor" "exclude-doctor-from-group"
reject_dependabot "generic frontend group competes for React DOM" "generic-overlap"
reject_dependabot "Expo major updates are no longer held" "remove-expo-major-ignore"
reject_dependabot "React Native major updates are no longer held" "remove-react-native-major-ignore"
reject_dependabot "NativeWind major updates are no longer held" "remove-nativewind-major-ignore"
reject_dependabot "Expo family major updates require wildcard coverage" "narrow-expo-major-ignore"
reject_dependabot "React Native family major updates require wildcard coverage" "remove-react-native-family-ignore"
reject_dependabot "React patches cannot be held by a wildcard ignore" "add-react-family-patch-ignore"
reject_dependabot "React Native family patches remain available" "add-react-native-family-patch-ignore"
reject_dependabot "React patches cannot be held by a version-specific ignore" "add-react-patch-version-ignore"
reject_dependabot "React patches cannot be held by an unconditional ignore" "add-unconditional-react-family-ignore"
reject_dependabot "React minor updates must remain manual" "remove-ignore-type:react:version-update:semver-minor"
reject_dependabot "React DOM minor updates must remain manual" "remove-ignore-type:react-dom:version-update:semver-minor"
reject_dependabot "React Native minor updates must remain manual" "remove-ignore-type:react-native:version-update:semver-minor"
reject_dependabot "React Native Web minor updates must remain manual" "remove-ignore-type:react-native-web:version-update:semver-minor"
reject_dependabot "NativeWind release candidates must hold every update" "remove-ignore-type:nativewind:version-update:semver-patch"
reject_dependabot "react-native-css release candidates must hold every update" "remove-ignore-type:react-native-css:version-update:semver-patch"

accept_dependency_policy "React patch updates remain available to Dependabot" "allowed-react-patch"
accept_dependency_policy "Expo 57 patch updates remain available to Dependabot" "set:mobile:dependencies:expo:~57.0.99"
accept_dependency_policy "Expo Router 57 patch updates remain available to Dependabot" "set:mobile:dependencies:expo-router:~57.0.99"
accept_dependency_policy "React Native 0.86 patch updates remain available to Dependabot" "set:mobile:dependencies:react-native:0.86.99"
accept_dependency_policy "React Native Web 0.21 patch updates remain available to Dependabot" "set:mobile:dependencies:react-native-web:~0.21.99"
accept_dependency_policy "TanStack Query 5 updates remain available to Dependabot" "set:mobile:dependencies:@tanstack/react-query:^5.102.0"
accept_dependency_policy "React Router 7 updates remain available to Dependabot" "set:web:dependencies:react-router:^7.14.0"
accept_dependency_policy "numeric prereleases are not mistaken for moving tags" "set:mobile:dependencies:react-native-reanimated:4.5.2-rc.0"

reject_dependency_policy "React Router 8 is incompatible with React 19.2.3" "set:web:dependencies:react-router:^8.3.0"
reject_dependency_policy "React Router requires a caret range" "set:web:dependencies:react-router:7.13.1"
reject_dependency_policy "React Router versions below 7.13.1 are unsupported" "set:web:dependencies:react-router:^7.12.0"
reject_dependency_policy "root React cannot drift from React DOM" "set:root:dependencies:react:19.2.4"
reject_dependency_policy "web React DOM cannot drift from the shared runtime" "set:web:dependencies:react-dom:19.2.4"
reject_dependency_policy "mobile React cannot drift from the shared runtime" "set:mobile:dependencies:react:19.2.4"
reject_dependency_policy "API-client React DOM cannot drift from the shared runtime" "set:api-client:devDependencies:react-dom:19.2.4"
reject_dependency_policy "moving latest tags are forbidden" "set:mobile:dependencies:expo:latest"
reject_dependency_policy "moving preview tags are forbidden" "set:mobile:dependencies:expo-router:preview"
reject_dependency_policy "wildcard dependency versions are forbidden" "set:mobile:dependencies:react-native:*"
reject_dependency_policy "empty dependency versions are forbidden" "set:mobile:dependencies:react-native-web:"
reject_dependency_policy "other unconstrained tags are forbidden" "set:mobile:dependencies:@tanstack/react-query:next"
reject_dependency_policy "Expo constants cannot use a moving tag" "set:mobile:dependencies:expo-constants:latest"
reject_dependency_policy "Reanimated cannot use a wildcard" "set:mobile:dependencies:react-native-reanimated:*"
reject_dependency_policy "Babel preset Expo cannot use a preview tag" "set:mobile:devDependencies:babel-preset-expo:preview"
reject_dependency_policy "new Expo compatibility dependencies cannot use moving tags" "set:mobile:dependencies:expo-camera:latest"
reject_dependency_policy "mobile compatibility dependencies cannot use git specs" "set:mobile:dependencies:@babel/runtime:git+https://example.com/runtime.git"
reject_dependency_policy "mobile compatibility dependencies cannot use URL specs" "set:mobile:dependencies:expo-linking:https://example.com/expo-linking.tgz"
reject_dependency_policy "mobile compatibility dependencies cannot use file specs" "set:mobile:dependencies:react-native-safe-area-context:file:../safe-area"
reject_dependency_policy "mobile compatibility dependencies require full semver" "set:mobile:dependencies:react-native-worklets:0.10"
reject_dependency_policy "numeric prerelease identifiers cannot have leading zeroes" "set:mobile:dependencies:react-native-reanimated:4.5.2-01"
reject_dependency_policy "numeric prerelease components cannot have leading zeroes" "set:mobile:dependencies:react-native-reanimated:4.5.2-rc.01"
reject_dependency_policy "React must be exact rather than ranged" "set:root:dependencies:react:^19.2.3"
reject_dependency_policy "Expo requires a tilde range" "set:mobile:dependencies:expo:57.0.1"
reject_dependency_policy "Expo Router requires a tilde range" "set:mobile:dependencies:expo-router:^57.0.1"
reject_dependency_policy "React Native must be exact" "set:mobile:dependencies:react-native:^0.86.3"
reject_dependency_policy "React Native Web requires a tilde range" "set:mobile:dependencies:react-native-web:0.21.1"
reject_dependency_policy "TanStack Query requires a caret range" "set:mobile:dependencies:@tanstack/react-query:5.102.0"
reject_dependency_policy "web TanStack Query must stay on caret major 5" "set:web:dependencies:@tanstack/react-query:latest"
reject_dependency_policy "API-client peer TanStack Query must stay on caret major 5" "set:api-client:peerDependencies:@tanstack/react-query:^6.0.0"
reject_dependency_policy "API-client dev TanStack Query must stay on caret major 5" "set:api-client:devDependencies:@tanstack/react-query:next"
reject_dependency_policy "coordinated React ranges are still forbidden" "coordinated-react-range"
reject_dependency_policy "coordinated React 19.3 upgrades require validation" "coordinated-react-minor"
reject_dependency_policy "Expo SDK 58 requires an explicit compatibility upgrade" "set:mobile:dependencies:expo:~58.0.0"
reject_dependency_policy "Expo Router must stay within SDK 57" "set:mobile:dependencies:expo-router:~58.0.0"
reject_dependency_policy "React Native 0.87 requires an explicit compatibility upgrade" "set:mobile:dependencies:react-native:0.87.0"
reject_dependency_policy "React Native Web 0.22 requires an explicit compatibility upgrade" "set:mobile:dependencies:react-native-web:~0.22.0"
reject_dependency_policy "TanStack Query must stay on major 5" "set:mobile:dependencies:@tanstack/react-query:^6.0.0"
reject_dependency_policy "NativeWind must use the validated release candidate" "set:mobile:dependencies:nativewind:5.0.0-rc.1"
reject_dependency_policy "react-native-css must use the validated prerelease" "set:mobile:dependencies:react-native-css:3.1.0-rc.1"
reject_dependency_policy "global React overrides are forbidden" "override:react:19.2.3"
reject_dependency_policy "global React DOM overrides are forbidden" "override:react-dom:19.2.3"
reject_dependency_policy "global lightningcss overrides are forbidden" "override:lightningcss:1.30.2"
reject_dependency_policy "version-qualified global overrides are forbidden" "qualified-override"
reject_dependency_policy "nested React overrides are forbidden" "nested-override"

reject_synthetic_react "a dynamic runtime React cannot drift" "dependencies" "react" "19.2.4"
reject_synthetic_react "a dynamic runtime React DOM cannot use a tag" "dependencies" "react-dom" "latest"
reject_synthetic_react "a dynamic development React cannot drift" "devDependencies" "react" "19.2.4"
reject_synthetic_react "a dynamic development React DOM cannot use a tag" "devDependencies" "react-dom" "latest"

echo "check-mobile-ci.test.sh: $pass passed, $fail failed"
if [ "$case_no" -ne "$TOTAL" ]; then
  bad "declared $TOTAL cases but executed $case_no"
fi
[ "$fail" -eq 0 ]
