#!/usr/bin/env bash
# Regression suite for scripts/check-portable-sed.sh.
#
# A guard that only ever runs against a clean tree is indistinguishable from one that always
# passes, so every rejection axis gets a fixture that constructs the violation and proves the
# guard rejects it. Case 5 is the one that matters most: without it, a typo in the exclusion
# lookup silently disables the WHOLE guard, and every other case would still pass.
#
# ⚠ bash 3.2-compatible (no mapfile/declare -A) -- see the guard's own header for why.
set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GUARD="$REPO_ROOT/scripts/check-portable-sed.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass=0; fail=0
ok()  { echo "  PASS: $1"; pass=$((pass+1)); }
bad() { echo "  FAIL: $1"; fail=$((fail+1)); }
mkfix() { mkdir -p "$(dirname "$TMP/$1")"; printf '%s\n' "$2" > "$TMP/$1"; }

# Portable octal file mode. `stat -f '%Lp'` is BSD/macOS; `stat -c '%a'` is GNU/Linux. This suite
# runs on both (locally on macOS, and in the e2e-test job on a Linux runner), so it must not
# hardcode either -- a portability suite that is itself unportable is a contradiction, and the
# first draft of case 8 shipped exactly that.
file_mode() {
    stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"
}

echo "1/9: every non-portable in-place form is rejected"
for form in "sed -i '' '/x/d' f" "sed -i'' '/x/d' f" "sed -i '/x/d' f" \
            "sed -i.bak '/x/d' f" "sed --in-place '/x/d' f" "sed -n -i '' '/x/d' f"; do
  rm -rf "$TMP/c1"; mkfix "c1/a.sh" "#!/bin/bash
$form"
  # Validate the fixture before trusting the verdict: an invalid mutation fakes both a hole
  # and a guard.
  if ! grep -qF -- "$form" "$TMP/c1/a.sh"; then bad "fixture invalid for: $form"; continue; fi
  if PORTABLE_SED_EXCLUDE= /bin/bash "$GUARD" "$TMP/c1" >/dev/null 2>&1; then
    bad "accepted: $form"
  else
    ok "rejected: $form"
  fi
done

echo "2/9: the portable temp-file form is accepted"
rm -rf "$TMP/c2"; mkfix "c2/a.sh" '#!/bin/bash
sed "/x/d" f > f.tmp && mv f.tmp f'
if PORTABLE_SED_EXCLUDE= /bin/bash "$GUARD" "$TMP/c2" >/dev/null 2>&1; then
  ok "accepted"
else
  bad "rejected the portable form -- the guard forbids its own prescribed fix"
fi

echo "3/9: a workflow run: body is in corpus"
rm -rf "$TMP/c3"; mkfix "c3/.github/workflows/w.yml" "jobs:
  j:
    steps:
      - run: sed -i '' '/x/d' f"
if PORTABLE_SED_EXCLUDE= /bin/bash "$GUARD" "$TMP/c3" >/dev/null 2>&1; then
  bad "workflow run: body not scanned -- the place a runner migration would add one"
else
  ok "workflow body scanned"
fi

echo "4/9: a Dockerfile and an extensionless shell-shebang script are in corpus"
rm -rf "$TMP/c4"; mkfix "c4/Dockerfile" "RUN sed -i '' '/x/d' f"
if PORTABLE_SED_EXCLUDE= /bin/bash "$GUARD" "$TMP/c4" >/dev/null 2>&1; then
  bad "Dockerfile not scanned"
else
  ok "Dockerfile scanned"
fi
rm -rf "$TMP/c4b"; mkfix "c4b/tool" "#!/usr/bin/env bash
sed -i '' '/x/d' f"
if PORTABLE_SED_EXCLUDE= /bin/bash "$GUARD" "$TMP/c4b" >/dev/null 2>&1; then
  bad "extensionless shell-shebang script not scanned"
else
  ok "shebang script scanned"
fi

echo "5/9: the exclusion accepts an excluded path and still rejects the same content elsewhere"
rm -rf "$TMP/c5"
mkfix "c5/apps/web/docker-entrypoint.sh" '#!/bin/sh
sed -i "s/A/B/" /etc/nginx/nginx.conf'
if PORTABLE_SED_EXCLUDE='apps/web/docker-entrypoint.sh' /bin/bash "$GUARD" "$TMP/c5" >/dev/null 2>&1; then
  ok "excluded path accepted"
else
  bad "exclusion did not fire -- the guard cannot be scoped, so it will be deleted rather than scoped"
fi
mkfix "c5/scripts/other.sh" '#!/bin/sh
sed -i "s/A/B/" /etc/nginx/nginx.conf'
if PORTABLE_SED_EXCLUDE='apps/web/docker-entrypoint.sh' /bin/bash "$GUARD" "$TMP/c5" >/dev/null 2>&1; then
  bad "exclusion leaked to a non-excluded path holding identical content"
else
  ok "same content still rejected outside the exclusion"
fi

echo "6/9: a COMMENT naming the anti-pattern is not flagged, but the same text as code is"
# The guard skips comment lines, because otherwise every file that documents the rule -- including
# the guard's own header -- reports as a violation of it. That skip is a bypass surface, so both
# directions are pinned here.
rm -rf "$TMP/c6"; mkfix "c6/a.sh" '#!/bin/bash
# Never write sed -i '"''"' EXPR FILE here; it is BSD-only.
echo fine'
if PORTABLE_SED_EXCLUDE= /bin/bash "$GUARD" "$TMP/c6" >/dev/null 2>&1; then
  ok "comment documenting the anti-pattern accepted"
else
  bad "a comment naming the anti-pattern was flagged -- documenting the rule would violate it"
fi

echo "7/9: an INDENTED real invocation is still flagged (the comment-skip must not over-reach)"
rm -rf "$TMP/c7"; mkfix "c7/a.sh" '#!/bin/bash
if true; then
    sed -i '"''"' "/x/d" f
fi'
if PORTABLE_SED_EXCLUDE= /bin/bash "$GUARD" "$TMP/c7" >/dev/null 2>&1; then
  bad "indented invocation slipped through the comment-skip"
else
  ok "indented invocation flagged"
fi

echo "8/9: the prescribed portable form preserves the file mode"
# The first version of this guard's own fix used `mv tmp file`, which gave the rewritten file the
# temp file's 0644 mode and silently stripped +x from scripts/test-migrations.sh -- a directly
# invoked script. The portability fix would have introduced a worse bug than the one it fixed.
# This pins the property the prescribed form must have.
rm -rf "$TMP/c8"; mkdir -p "$TMP/c8"
printf '#!/bin/bash\necho keep-me\n' > "$TMP/c8/exec.sh"
chmod 755 "$TMP/c8/exec.sh"
before="$(file_mode "$TMP/c8/exec.sh")"
# Exactly the form the guard's failure message prescribes.
sed 's/keep-me/kept/' "$TMP/c8/exec.sh" > "$TMP/c8/exec.sh.tmp" \
  && cat "$TMP/c8/exec.sh.tmp" > "$TMP/c8/exec.sh" \
  && rm -f "$TMP/c8/exec.sh.tmp"
after="$(file_mode "$TMP/c8/exec.sh")"
if [ "$before" = "$after" ] && grep -q kept "$TMP/c8/exec.sh"; then
  ok "mode preserved ($before) and content rewritten"
else
  bad "prescribed form changed mode $before -> $after, or did not rewrite content"
fi

echo "9/9: the real tracked tree is clean"
if (cd "$REPO_ROOT" && /bin/bash "$GUARD" >/dev/null 2>&1); then
  ok "tree clean"
else
  bad "tracked tree has non-portable in-place sed"
fi

echo "check-portable-sed.test.sh: $pass passed, $fail failed"
[ "$fail" -eq 0 ]
