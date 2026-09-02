#!/bin/bash
# GitHub Self-Hosted Runner Maintenance Script
# Runs twice-daily via cron: 0 2,14 * * * ~/github-runners/runner-maintenance.sh

set -euo pipefail

# CRITICAL: Full PATH for all tools including nvm
# /opt/homebrew/opt/util-linux/bin is REQUIRED, not decorative: flock lives there and is
# keg-only, so Homebrew does NOT symlink it into /opt/homebrew/bin. Verified: that symlink is
# absent, and with the previous PATH `command -v flock` returned nothing under a minimal
# cron environment while `timeout` resolved fine. The host-GC lock below therefore silently
# took the "flock unavailable" branch on every cron run and serialized nothing — it only
# appeared to work when tested from an interactive shell, which has util-linux on PATH.
export PATH="/opt/homebrew/bin:/opt/homebrew/opt/util-linux/bin:/Users/runner/.nvm/versions/node/v22.21.0/bin:/Applications/Docker.app/Contents/Resources/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"

LOG_FILE="${HOME}/github-runners/maintenance.log"
DATE=$(date '+%Y-%m-%d %H:%M:%S')
DISK_ALERT_THRESHOLD=70  # percent of the DATA volume — see disk_used_pct below

# Measure the DATA volume, not `/`.
#
# On APFS `/` is the sealed read-only system volume (/dev/disk3s1s1); everything CI writes
# — checkouts, Docker, ~/Library/Caches — lives on /dev/disk3s5 at /System/Volumes/Data.
# Observed simultaneously:
#     /dev/disk3s1s1  228Gi   10Gi  44Gi  19%  /
#     /dev/disk3s5    228Gi  164Gi  44Gi  79%  /System/Volumes/Data
#
# macOS df reports %Capacity as used/(used+avail) (BSD df: used = blocks - bfree,
# denominator used + bavail), NOT used/total. Verified against raw blocks: the system
# volume showed 10899096/(10899096+24905624) = 30.4% -> "31%", while Data showed
# 190590088/(190590088+24905624) = 88.4% -> "89%". Both volumes share the container's free
# space, so a reading on `/` is not unrelated to real pressure — it is a badly distorted
# proxy for it, because the numerator is the system volume's own usage.
#
# That distortion is severe and NOT a fixed offset: the system volume's `used` was ~10Gi
# after recovery but ~17Gi under pressure, moving the point where `/` crosses 70% between
# roughly 4.3Gi and 7.3Gi free. Either way it only trips near exhaustion, which is far too
# late for four concurrent runners.
#
# Scope note: inside THIS script the 70% figure is only a post-run warning — the cleanup
# steps run unconditionally. The thresholds that actually gated action lived in crontab,
# and those never executed at all (see disk-guard.sh for why).
#
# Incident timeline for the record (2026-08-04, local time): 67% logged at 14:00, 96% by
# 19:05, and 100% with ~121MiB free at 19:08 when actions/checkout died with
# "ENOSPC: no space left on device". Roughly five hours from a reassuring log line to
# failure — and the log line was reassuring only because it measured the wrong volume.
DATA_VOLUME="/System/Volumes/Data"

# Percent used on the data volume, or 0 if it cannot be read. NEVER fails.
#
# Same `set -euo pipefail` trap as get_size_mb: a bare `X=$(disk_used_pct)` aborts the whole run
# if `df` fails, and this is the FIRST measurement the script takes — so an unreadable volume
# would kill maintenance before a single reclaim, at exactly the moment it is most needed.
# Verified with a repro against a nonexistent mount point (exit 1, next line unreachable).
#
# Returns 0 rather than empty so arithmetic comparisons at the call sites stay valid; a
# genuinely unreadable volume is called out in the log instead of silently reading as "empty".
disk_used_pct() {
    local out
    out=$(df -h "$DATA_VOLUME" 2>/dev/null | awk 'NR==2 {gsub(/%/,""); print $5}') || out=""
    case "${out:-}" in
        ''|*[!0-9]*)
            echo "[$DATE] WARNING: could not read usage for ${DATA_VOLUME} — reporting 0" >> "$LOG_FILE"
            printf '0'
            return 0
            ;;
    esac
    printf '%s' "$out"
}

# --aggressive: drop the age filters.
#
# Every routine reclaim is age-gated (docker `until=24h`, `-mtime +1`, `-mtime +3`), which
# is right for a scheduled sweep but useless in the emergency it is supposed to answer:
# when the disk fills DURING a CI burst, everything consuming it is minutes old and every
# filter excludes it. Observed 2026-08-04 20:15 at 89% — a full run reclaimed 0B from
# every single step, then logged "Consider manual intervention or more aggressive cleanup"
# with no way to do so.
#
# Aggressive mode only ever touches regenerable data: Docker's UNUSED images/cache (the
# daemon refuses to remove anything a running container holds), vulnerability DBs, and
# compiler/tool caches. It deliberately does NOT touch _work/workspace-* beyond the usual
# 3-day rule — those can belong to an in-flight job, and deleting a live checkout would
# corrupt a running run to save space.
AGGRESSIVE=0
if [ "${1:-}" = "--aggressive" ]; then
    AGGRESSIVE=1
fi

# True when no runner is currently executing a job.
#
# Runner.Listener runs permanently (one per runner); Runner.Worker exists only for the
# duration of an actual job, so it is the correct "busy" signal. Caches like Grype's
# vulnerability DB are SHARED across all four runners, so deleting one mid-scan can fail or
# corrupt a live job. Destructive cache removal is gated on this; age-filtered pruning of
# unused data is not, since Docker refuses to remove what a running container holds.
runners_idle() {
    # Fail CLOSED. `! pgrep ...` collapsed three outcomes into two: pgrep exits 0 when a
    # worker matches, 1 when none does, and 2/3 on an internal error or bad usage. The negation
    # treated an ERROR exactly like "no jobs running", so a transient failure to read the
    # process list would green-light deleting caches out from under live jobs. This is a safety
    # predicate, so anything other than a clean "no match" must mean busy.
    local rc=0
    pgrep -f "Runner.Worker" >/dev/null 2>&1 || rc=$?
    case "$rc" in
        0) return 1 ;;  # matched -> a job is executing -> not idle
        1) return 0 ;;  # clean "no match" -> genuinely idle
        *) log "runners_idle: pgrep failed (exit ${rc}) — assuming BUSY (fail closed)"
           return 1 ;;
    esac
}

# Logging must NEVER be able to kill the run.
#
# This was `echo "..." | tee -a "$LOG_FILE"` — an unguarded pipeline under `set -euo pipefail`.
# If appending to the log fails, the pipeline fails and the script exits. The realistic trigger is
# ENOSPC on the very volume this script exists to reclaim: with the disk full, the FIRST log call
# ("=== Starting runner maintenance ===") would terminate maintenance before a single byte was
# freed. The tool would be disabled by exactly the condition it is meant to fix.
#
# stdout and the file append are now separate and both best-effort, and the function always
# returns 0 so callers can never inherit a failure.
log() {
    printf '[%s] %s\n' "$DATE" "$1" 2>/dev/null || true
    { printf '[%s] %s\n' "$DATE" "$1" >> "$LOG_FILE"; } 2>/dev/null || true
    return 0
}

# Size of a directory in MB, or 0 if it is missing/unreadable. NEVER fails.
#
# The `|| out=""` is load-bearing under `set -euo pipefail`. `du` on a missing path exits
# nonzero, and with pipefail the whole `du | awk` pipeline inherits that, so a bare
# `x=$(get_size_mb "$gone")` ABORTS THE SCRIPT. That is not theoretical: purge_grype_cache
# measures the directory immediately after `rm -rf` succeeds, so the success path of the
# emergency Grype reclaim killed the run before any later cleanup — verified with a minimal
# repro (exit 1, statement after the assignment never reached). Returning 0 for "not there"
# is also the semantically right answer for a size query.
get_size_mb() {
    local dir="$1" out
    out=$(du -sm "$dir" 2>/dev/null | awk 'NR==1 {print $1}') || out=""
    printf '%s' "${out:-0}"
}

clean_old_symlinked_versions() {
    local link_name="$1"
    for runner in "${HOME}/github-runners/runner-"{1,2,3,4}; do
        if [ -L "${runner}/${link_name}" ]; then
            # Guarded for the same reason as the other pipeline assignments: a readlink/xargs
            # failure here would abort the entire run rather than skip one runner.
            current=$(readlink "${runner}/${link_name}" | xargs basename) || current=""
            [ -n "$current" ] || { log "  ${link_name}: could not resolve symlink target in $(basename "$runner") — skipping"; continue; }
            for old in "${runner}"/${link_name}.*/; do
                if [ -d "$old" ]; then
                    old_name=$(basename "$old")
                    if [ "$old_name" != "$current" ]; then
                        log "Removing old ${link_name}: $old_name from $(basename $runner)"
                        rm -rf "$old"
                    fi
                fi
            done
        fi
    done
}

# Remove a Grype DB cache and VERIFY the bytes actually went away.
#
# `grype db delete` is the official, layout-aware path and is tried first. It is not TRUSTED,
# though: the check below confirms the outcome instead of believing the command's own success
# message. That is a deliberate stance rather than an accusation — a previous revision of this
# comment asserted the command "freed ZERO bytes", based on a test that had pointed
# GRYPE_DB_CACHE_DIR one level too high (at the cache parent rather than its `db` directory)
# with a schema-5 fixture against a schema-6 binary. The test was invalid and the claim is
# withdrawn; whether the command reclaims correctly when properly targeted is unproven here.
#
# Verifying anyway is still right, because the failure that caused this whole incident was a
# maintenance run reporting a reassuring "67%" while the disk filled to ENOSPC. On the one
# reclaim path that matters most, confirm rather than assume.
purge_grype_cache() {
    local dir="$1" before after leftover_db
    before=$(get_size_mb "$dir")

    # GRYPE_DB_CACHE_DIR must point at the DB directory itself, not its parent. `grype config`
    # reports the default as '~/Library/Caches/grype/db'. An earlier revision passed the
    # parent, one level too high, and I wrongly concluded from the resulting no-op that
    # `grype db delete` does not reclaim at all. It was pointed at the wrong directory (and
    # the hand-made fixture used schema 5 while the installed Grype uses schema 6), so that
    # experiment proved nothing about the command. Claim retracted.
    if command -v grype >/dev/null 2>&1 && [ -d "${dir}/db" ]; then
        GRYPE_DB_CACHE_DIR="${dir}/db" timeout --kill-after=10s 120 grype db delete 2>&1 | tee -a "$LOG_FILE" || true
    fi

    # Verify by ARTIFACT, not by a size threshold. The previous "still over 50MB" rule would
    # silently report a surviving 40MB vulnerability.db as reclaimed. Any remaining *.db means
    # the official path did not do the job, whatever it printed.
    # `-print -quit` instead of `| head -1`.
    #
    # The pipe form had a false-negative: `head` closes the pipe after one line, `find` can then
    # die of SIGPIPE, pipefail propagates that, and the `|| leftover_db=""` guard DISCARDS the
    # filename it just found — so a surviving database would be reported as successfully reclaimed.
    # Guarding the abort was necessary but not sufficient; the verification semantics were wrong.
    # `-print -quit` is BSD-supported and stops find itself, with no pipe to break.
    #
    # Caveat kept honest: this proves the *.db files are gone, not that every large archive or
    # temporary artifact under the cache was reclaimed. The before/after sizes are logged below.
    leftover_db=$(find "$dir" -type f -name '*.db' -print -quit 2>/dev/null) || leftover_db=""
    if [ -n "$leftover_db" ]; then
        log "  DB file still present after 'grype db delete' ($(basename "$leftover_db")) — removing ${dir}"
        rm -rf "$dir" 2>/dev/null || true
    fi

    after=$(get_size_mb "$dir")
    log "  grype cache: ${before}MB -> ${after}MB"
}

# Cheap, high-value reclaim. In aggressive mode this runs BEFORE the Docker steps.
#
# Ordering matters in the emergency this mode exists for. The Docker steps carry up to ~16
# minutes of combined timeout budget and a wedged daemon burns all of it — yet the largest,
# most reliable wins are not in Docker at all: the Grype DB measured 6.7GB and the Go build
# cache 3.7GB, both removable in seconds. Waiting on a hung daemon before touching >10GB of
# regenerable cache is exactly backwards when a checkout is about to die with ENOSPC.
#
# Idle-gated as a whole: every path here is shared with executing jobs. Deleting a
# vulnerability DB under a live scan, running `npm cache clean` during a dependency install,
# or clearing GOCACHE under a running `go build` trades a disk problem for a corrupted run.
aggressive_cheap_reclaim() {
    if ! runners_idle; then
        log "Jobs are RUNNING — skipping shared-cache reclaim (vuln DBs, tool caches, npm, GOCACHE)"
        return 0
    fi

    for vuln_dir in "${HOME}/Library/Caches/grype" "${HOME}/Library/Caches/trivy"; do
        [ -d "$vuln_dir" ] || continue
        vuln_mb=$(get_size_mb "$vuln_dir")
        if [ "$(basename "$vuln_dir")" = "grype" ]; then
            log "Purging Grype DB (${vuln_mb:-?}MB, re-downloaded on next scan)"
            purge_grype_cache "$vuln_dir"
        else
            log "Clearing $(basename "$vuln_dir") cache (${vuln_mb:-?}MB, re-downloaded on next scan)"
            rm -rf "$vuln_dir" 2>/dev/null || true
        fi
    done

    # Compiler/tooling caches — rebuilt on demand, at the cost of one slower build.
    for tool_cache in "${HOME}/Library/Caches/gopls" "${HOME}/Library/Caches/goimports"; do
        [ -d "$tool_cache" ] || continue
        tc_mb=$(get_size_mb "$tool_cache")
        log "Clearing $(basename "$tool_cache") cache (${tc_mb:-?}MB)"
        rm -rf "$tool_cache" 2>/dev/null || true
    done

    log "Clearing npm cache..."
    timeout --kill-after=10s 120 npm cache clean --force 2>&1 | tee -a "$LOG_FILE" || true

    # Also ahead of Docker: step 10 cleans this unconditionally later, but in an emergency
    # multiple GB should not wait behind a possibly-hung daemon. The repeat is a cheap no-op.
    if command -v go >/dev/null 2>&1; then
        log "Clearing Go build cache (ahead of Docker)..."
        timeout --kill-after=10s 120 go clean -cache 2>&1 | tee -a "$LOG_FILE" || true
    fi
}

# Serialize host GC — only one maintenance run at a time.
#
# Three independent things invoke this script: cron at 02:00/14:00, disk-guard.sh every 4
# hours, and a human during an incident. A single run can now legitimately take many minutes
# (the Docker steps alone allow up to ~16 minutes of timeout budget), so overlap is real
# rather than theoretical. Two concurrent runs would race the same Docker daemon and the same
# cache directories — one deleting a vulnerability DB while the other measures it, both
# pruning at once — which is precisely the unserialized host GC that makes reclaim
# unpredictable and can surface as a corrupted scan.
#
# Non-blocking (`-n`): if another run holds the lock, exit quietly rather than queueing. A
# second sweep immediately after the first has nothing left to reclaim, and queueing would
# just stack runs during exactly the pressure window we care about. The lock releases
# automatically when fd 9 closes on exit, including on kill, so no stale lockfile handling is
# needed (unlike a mkdir/PID-file scheme).
LOCK_FILE="${HOME}/github-runners/.maintenance.lock"
# Resolve flock explicitly. It is keg-only, so PATH alone was not enough historically (see
# the PATH comment at the top); fall back to the known Homebrew keg path.
FLOCK_BIN=""
if command -v flock >/dev/null 2>&1; then
    FLOCK_BIN=$(command -v flock)
elif [ -x /opt/homebrew/opt/util-linux/bin/flock ]; then
    FLOCK_BIN=/opt/homebrew/opt/util-linux/bin/flock
fi

# Exit 75 (EX_TEMPFAIL), not 0, when another run holds the lock.
#
# Exiting 0 made a skipped run indistinguishable from a completed one: disk-guard would log
# "maintenance complete" and, worse, an --aggressive request issued during a disk emergency
# would vanish silently if a routine run happened to hold the lock. The caller needs to know
# the work did not happen so it can retry or escalate.
# `-E 75` makes 75 mean CONTENTION SPECIFICALLY. Without it, every flock failure — a bad fd, an
# unwritable lock path — collapsed into the same 75 that disk-guard interprets as "another run has
# the lock, retry later", so a real malfunction would be retried forever instead of reported.
# Residual ambiguity, documented rather than hidden: exit codes are not namespaced, so a command in
# the body that itself exits 75 would abort under `set -e` with 75 and be misread as contention.
# Nothing in the current body does that, and -E removes flock as a source of the collision.
LOCK_BUSY_EXIT=75
if [ -n "$FLOCK_BIN" ]; then
    exec 9>"$LOCK_FILE"
    lock_rc=0
    "$FLOCK_BIN" -n -E "$LOCK_BUSY_EXIT" 9 || lock_rc=$?
    if [ "$lock_rc" -eq "$LOCK_BUSY_EXIT" ]; then
        log "maintenance: another run holds ${LOCK_FILE} — exiting ${LOCK_BUSY_EXIT} (work NOT done)"
        exit "$LOCK_BUSY_EXIT"
    elif [ "$lock_rc" -ne 0 ]; then
        log "maintenance: flock failed with exit ${lock_rc} (NOT contention) — proceeding WITHOUT host GC serialization"
    fi
else
    log "maintenance: flock NOT FOUND (checked PATH and /opt/homebrew/opt/util-linux/bin) — proceeding WITHOUT host GC serialization"
fi

# Rotate log if > 1MB
if [ -f "$LOG_FILE" ] && [ "$(stat -f%z "$LOG_FILE" 2>/dev/null || echo 0)" -gt 1048576 ]; then
    mv "$LOG_FILE" "${LOG_FILE}.old"
fi

log "=== Starting runner maintenance ==="

# 1. Check disk BEFORE cleanup
DISK_USED=$(disk_used_pct)
log "Disk usage before cleanup: ${DISK_USED}% of ${DATA_VOLUME}"

# Emergency first: reclaim the big cheap wins before spending timeout budget on Docker.
if [ "$AGGRESSIVE" -eq 1 ]; then
    log "--- AGGRESSIVE MODE: cheap reclaim first (regenerable data only) ---"
    aggressive_cheap_reclaim
    log "--- Cheap reclaim done: $(disk_used_pct)% used — proceeding to Docker ---"
fi

# Every Docker step below is wrapped in `timeout` and ends in `|| true`.
#
# `set -euo pipefail` (line 5) plus a `| tee` pipeline meant a single FAILING Docker
# command aborted the ENTIRE script, silently skipping every later reclaim: the _tool
# sweep, the vulnerability DBs, `go clean -cache`, the workspace sweep, the diag-log sweep.
# Docker being unhealthy is exactly when the disk is under pressure, so the cheap and
# reliable reclaims were the ones lost. (Not every historical run hit this — the 14:00 run
# on 2026-08-04 completed all steps — but the logs do contain Docker 500 errors followed by
# no completion marker.)
#
# `|| true` alone is not enough: it catches a command that FAILS, never one that HANGS, and
# `docker system df` hung for over 300s on this host during the incident. A hung call with
# no timeout would wedge maintenance indefinitely, which is why each gets an explicit bound.
#
# Still imperfect by design: these prunes race the four runners' own post-job cleanup hooks
# against one shared daemon. Pruning only removes UNUSED objects, so the race is wasteful
# rather than harmful, but a host-wide lock would be the real fix.

# 2. Remove stopped containers older than 1 day
log "Cleaning stopped containers..."
timeout --kill-after=10s 180 docker container prune -f --filter "until=24h" 2>&1 | tee -a "$LOG_FILE" || true

# 3/4. Images and build cache.
#
# Skipped entirely in aggressive mode: step 10b prunes both again with a filter that is equal
# (busy: until=24h) or strictly wider (idle: no filter), so running them here first is pure
# duplication — and expensive duplication, since each carries a 300s timeout budget and the
# emergency path is exactly where minutes matter. Containers and volumes (steps 2 and 5) are NOT
# repeated in 10b, so they always run.
if [ "$AGGRESSIVE" -eq 1 ]; then
    log "Images/build cache: deferred to the aggressive tier (avoids a duplicate 10-minute pass)"
else
    log "Cleaning unused Docker images older than 24 hours..."
    timeout --kill-after=10s 300 docker image prune -af --filter "until=24h" 2>&1 | tee -a "$LOG_FILE" || true

    log "Cleaning build cache..."
    timeout --kill-after=10s 300 docker buildx prune -f --filter "until=72h" 2>&1 | tee -a "$LOG_FILE" || true
fi

# 5. Remove unused volumes
log "Cleaning unused volumes..."
timeout --kill-after=10s 180 docker volume prune -f 2>&1 | tee -a "$LOG_FILE" || true

# 6. Clean _tool cache - remove old tool versions (keep latest 2)
#
# Idle-gated. This was an UNGATED deletion of a shared directory: _work/_tool is the runner's
# hosted-tool cache, and a job resolving a toolchain (actions/setup-node, setup-go, setup-python)
# reads from it. Removing an older version mid-job can pull the interpreter out from under a step
# that is currently using it. "Keep newest 2" reduces but does not eliminate that — a job pinned
# to an older minor is exactly the case that gets deleted.
if ! runners_idle; then
    log "_tool cache: jobs are RUNNING — skipping (a job may be resolving a toolchain from it)"
else
log "Cleaning old tool versions from _tool cache..."
for runner_dir in "${HOME}/github-runners/runner-1" "${HOME}/github-runners/runner-2" "${HOME}/github-runners/runner-3" "${HOME}/github-runners/runner-4"; do
    tool_dir="${runner_dir}/_work/_tool"
    if [ -d "$tool_dir" ]; then
        for tool in "$tool_dir"/*/; do
            if [ -d "$tool" ]; then
                # Keep newest 2 versions, remove rest
                ls -dt "${tool}"*/ 2>/dev/null | tail -n +3 | xargs -r rm -rf 2>/dev/null || true
            fi
        done
    fi
done
log "Tool cache cleanup complete"
fi

# 7. Clean Trivy vulnerability databases (regenerated on next scan) — idle-gated.
#
# Retained for hosts/branches that still run Trivy, but note step 7b: this repo's CI no longer uses
# Trivy, so on its own this step reclaims nothing here. Gated anyway: it was another ungated
# deletion of a per-runner cache that a scan could be reading, and "it currently matches nothing"
# is not a safety property — it becomes wrong the moment a branch reintroduces Trivy.
if ! runners_idle; then
    log "Trivy caches: jobs are RUNNING — skipping (a scan may be reading them)"
else
    log "Cleaning Trivy caches older than 1 day..."
    for runner in "${HOME}/github-runners/runner-"{1,2,3,4}; do
        find "$runner" -path "*/.cache/trivy/*" -type f \( -name "trivy.db" -o -name "trivy-java.db" \) -mtime +1 -delete 2>/dev/null || true
    done
    log "Trivy cache cleanup complete"
fi

# 7b. Clean Grype vulnerability databases — the scanner this repo's CI actually runs.
#
# The `docker-image-scan` job in .github/workflows/test.yml brew-installs Grype and scans each
# component image; there is no active Trivy invocation anywhere in the workflow, so step 7 above
# reclaims nothing here. (That job's header comment says Trivy was replaced by "official/native
# tools" generally — Grype is a separate job, not literally named as its replacement.)
#
# ⚠ Both citations in this paragraph were LINE NUMBERS until #1598, and both were already wrong
# before it: the job was cited as "~line 1568" when it was at 2674, and the Trivy comment as
# "test.yml:1483" when 1483 was an unrelated `runs-on:` line. Nothing caught either, because
# TestCitedLineNumbersAreInRange only checks that a cited line is IN RANGE, and test.yml is ~3000
# lines long. Cite this file by job name — it survives every edit above it, which line numbers in
# a workflow this size demonstrably do not.
#
# Grype caches under
# ~/Library/Caches/grype/db, outside ~/github-runners, which was the entire sweep area —
# so the real consumer went untouched. It was measured at 6.7GB on 2026-08-04 and cleared
# during incident response; since that removal happened before this step existed, treat
# 6.7GB as a one-off observation rather than a verified steady state.
#
# The DB is pure cache — Grype re-downloads it on the next scan — so clearing costs one
# download, not correctness. Two guards nonetheless:
#   * size-triggered, so routine 12-hourly runs don't force a re-download every time;
#   * idle-gated, because this cache is SHARED by all four runners and `rm -rf` under a
#     live `grype` scan can fail or corrupt that job. `grype db delete` is preferred when
#     available since it understands the layout.
GRYPE_MAX_MB=2048
log "Checking Grype vulnerability DB caches..."
for grype_dir in "${HOME}/Library/Caches/grype" "${HOME}/github-runners/runner-"{1,2,3,4}"/.cache/grype"; do
    [ -d "$grype_dir" ] || continue
    grype_mb=$(get_size_mb "$grype_dir")
    case "${grype_mb:-0}" in ''|*[!0-9]*) grype_mb=0 ;; esac
    if [ "$grype_mb" -le "$GRYPE_MAX_MB" ]; then
        log "  ${grype_dir}: ${grype_mb}MB — under ${GRYPE_MAX_MB}MB, keeping"
    elif ! runners_idle; then
        log "  ${grype_dir}: ${grype_mb}MB over threshold, but a job is RUNNING — deferring (a scan may be reading it)"
    else
        log "  ${grype_dir}: ${grype_mb}MB over threshold and host idle — purging"
        purge_grype_cache "$grype_dir"
    fi
done
log "Grype cache cleanup complete"

# 8. NEW: Clean old externals versions (keep only current symlinked version)
log "Cleaning old externals versions..."
clean_old_symlinked_versions "externals"

# 9. NEW: Clean old bin versions (keep only current symlinked version)
log "Cleaning old bin versions..."
clean_old_symlinked_versions "bin"

# 10. Clean Go build cache daily — idle-gated and bounded.
#
# This step used to run unconditionally, which made the idle gate in
# aggressive_cheap_reclaim() PURELY DECORATIVE: a run could log
# "Jobs are RUNNING — skipping shared-cache reclaim (... GOCACHE)" and then wipe GOCACHE
# thirty lines later anyway. That exact sequence is in this log (the "skipping" line
# immediately followed by "Cleaning Go build cache"). Clearing GOCACHE under a live `go build`
# can fail that build, so the gate has to apply everywhere or it protects nothing.
#
# Also bounded: this was one of the unguarded, unbounded calls, and `go clean` walks the whole
# cache tree — on a loaded host that is exactly where a stall would strand the run.
#
# Skipped entirely when AGGRESSIVE already cleared it before the Docker steps, so an
# emergency run does not walk the tree twice.
if [ "$AGGRESSIVE" -eq 1 ]; then
    # Accurate wording matters: the aggressive phase CLEARS this when idle and deliberately
    # SKIPS it when jobs are running. Saying "already handled" would assert a reclaim that may
    # never have happened — the same kind of false-reassurance log line that let this host fill
    # up while reporting healthy numbers.
    log "Go build cache: delegated to the aggressive cheap-reclaim phase (cleared if idle, skipped if busy)"
elif ! runners_idle; then
    log "Go build cache: jobs are RUNNING — skipping (clearing GOCACHE can fail a live build)"
elif command -v go >/dev/null 2>&1; then
    log "Cleaning Go build cache..."
    timeout --kill-after=10s 120 go clean -cache 2>&1 | tee -a "$LOG_FILE" || true
fi

# 10b. AGGRESSIVE TIER — only when invoked with --aggressive (see header).
# Regenerable data only; nothing here can corrupt an in-flight job.
if [ "$AGGRESSIVE" -eq 1 ]; then
    log "--- AGGRESSIVE MODE: dropping age filters (regenerable data only) ---"

    # "Unused" is NOT the same as "safe to delete" while jobs run.
    #
    # docker image prune only skips images held by a running CONTAINER — but the Docker
    # Image Scan job builds test-<component>:<sha> and then invokes `grype` on it, and in
    # that window the image belongs to no container at all. A fully unfiltered prune would
    # delete the image out from under the scan and fail the job. Same for buildx cache: a
    # concurrent build is actively populating it.
    #
    # So while any job is executing, keep a 1h floor — still vastly more aggressive than
    # the routine 24h, but it cannot reach anything the current run just produced. Drop the
    # filter entirely only when the host is idle. Bounded by `timeout`: `|| true` catches a
    # failing daemon, never a hanging one.
    # While jobs run, do NOT go below the routine 24h floor.
    #
    # An earlier revision used `until=1h` here, reasoning that an hour was enough to protect an
    # image between its build and its scan. That is false on this host: a single
    # "Docker Image Scan (frontend)" worker was measured running for over ELEVEN HOURS in the
    # runner diagnostics, so its built image can be many hours old while the job is still very
    # much alive. Pruning it would fail that job to reclaim space. 24h is the long-standing
    # routine floor and stays the floor whenever anything is executing; aggression here buys
    # little anyway, since images minutes old are the ones filling the disk and they are
    # untouchable either way.
    #
    # Known residual race, NOT fixed here: runners_idle is point-in-time. A listener can accept
    # a new job immediately after the check and while the unfiltered prune runs. Closing that
    # needs job-admission control at the runner level (a pre-job hook refusing work while GC
    # holds the lock), which a cron script cannot provide.
    if runners_idle; then
        prune_filter=""
        log "Host idle — pruning ALL unused Docker images (no age filter)..."
    else
        prune_filter="--filter until=24h"
        log "Jobs RUNNING — holding the routine 24h floor (an image-scan job can run 11+ hours)..."
    fi
    # shellcheck disable=SC2086
    timeout --kill-after=10s 300 docker image prune -af $prune_filter 2>&1 | tee -a "$LOG_FILE" || true

    # shellcheck disable=SC2086
    log "Pruning Docker build cache${prune_filter:+ older than 1h}..."
    # shellcheck disable=SC2086
    timeout --kill-after=10s 300 docker buildx prune -af $prune_filter 2>&1 | tee -a "$LOG_FILE" || true

    # The shared-cache reclaim already ran, before the Docker steps — see
    # aggressive_cheap_reclaim() and the call site just after the initial disk reading.
    log "--- AGGRESSIVE Docker prunes done (cheap reclaim already ran ahead of them) ---"

    log "--- AGGRESSIVE MODE complete: $(disk_used_pct)% used ---"
fi

# 11. NEW: Rotate post-job cleanup log if > 1MB
POSTJOB_LOG="${HOME}/github-runners/post-job-cleanup.log"
if [ -f "$POSTJOB_LOG" ] && [ "$(stat -f%z "$POSTJOB_LOG" 2>/dev/null || echo 0)" -gt 1048576 ]; then
    tail -1000 "$POSTJOB_LOG" > "${POSTJOB_LOG}.tmp" && mv "${POSTJOB_LOG}.tmp" "$POSTJOB_LOG"
    log "Post-job log truncated"
fi

# 12. Clean diagnostic logs older than 3 days (CHANGED from 7)
log "Cleaning diagnostic logs older than 3 days..."
find "${HOME}/github-runners/runner-1/_diag" "${HOME}/github-runners/runner-2/_diag" "${HOME}/github-runners/runner-3/_diag" "${HOME}/github-runners/runner-4/_diag" -name "*.log" -mtime +3 -delete 2>&1 | tee -a "$LOG_FILE" || true

# 8. Clean workspace directories older than 3 days
#
# `-prune` is the actual fix: without it find recurses INTO each directory it has just
# rm -rf'd and reports "No such file or directory" for every path underneath — 1094 such
# messages accumulated in this log historically. The deletion itself still worked and
# siblings were still visited, so this was noise rather than lost reclaim; `|| true` hid it.
#
# Depth: workspaces are NOT direct children of _work. The runner checks out under
# _work/<repo>/<repo>/, so real paths look like
#     _work/BoostedBudget/BoostedBudget/workspace-e2e-test-<run_id>
# i.e. depth 3. An earlier revision of this comment asserted depth 1 and set
# `-maxdepth 1`, which matched nothing and silently disabled workspace GC entirely —
# strictly worse than the noisy original. -maxdepth 4 keeps the walk bounded (no descent
# into node_modules) while still covering depth 3 with margin.
log "Cleaning old workspace directories..."
find "${HOME}/github-runners/runner-1/_work" "${HOME}/github-runners/runner-2/_work" "${HOME}/github-runners/runner-3/_work" "${HOME}/github-runners/runner-4/_work" \
    -maxdepth 4 -type d -name "workspace-*" -mtime +3 -prune -exec rm -rf -- {} + 2>&1 | tee -a "$LOG_FILE" || true

# 9/10. Weekly (Sunday) npm + Homebrew cache cleanup — idle-gated and bounded.
#
# Both were unbounded, and `npm cache clean --force` during a job's dependency install can
# fail that install, so they get the same idle gate as every other shared-cache operation.
# The aggressive phase already handles npm, so skip the duplicate there.
if [ "$(date +%u)" -eq 7 ]; then
    if ! runners_idle; then
        log "Weekly npm/Homebrew cleanup: jobs are RUNNING — skipping (shared caches)"
    else
        if [ "$AGGRESSIVE" -eq 1 ]; then
            log "Weekly npm cleanup: already handled by the aggressive cheap-reclaim phase"
        else
            log "Weekly npm cache cleanup..."
            timeout --kill-after=10s 120 npm cache clean --force 2>&1 | tee -a "$LOG_FILE" || true
        fi
        log "Weekly homebrew cache cleanup..."
        timeout --kill-after=10s 300 brew cleanup -s 2>&1 | tee -a "$LOG_FILE" || true
    fi
fi

# 11. Docker store status. Deliberately does NOT restart Docker.
#
# This call used to be a bare `docker system df | tee`, and it was the single most dangerous
# line in the script: it is the exact command that hung for over 300s on this host during
# the 2026-08-04 incident, and under `set -euo pipefail` a hang here wedges maintenance
# indefinitely while a failure aborts the remaining steps (the post-cleanup disk reading and
# the threshold warning below). Now bounded and non-fatal.
#
# It also replaces a restart heuristic that parsed
# `docker system df --format {{.Size}} | head -1 | sed s/GB// | cut -d. -f1`
# and quit Docker Desktop above "10". Three separate faults:
#
#   * The unit was assumed to be GB, and `head -1` is the IMAGES row (verified: this prints
#     "Images=1.43GB" first, not a total). So "408.4MB" survived sed, became "408" after
#     cut, and compared as 408 > 10. This ACTUALLY HAPPENED — the historical entry in this
#     log where an active 408.4MB image store triggered a restart. A 1.5TB store would
#     conversely read as "1" and be ignored.
#   * A non-numeric value does NOT abort the script: `[ "512MB" -gt 10 ]` prints an error
#     but sits in an `if` condition, and `set -e` does not exit on a failing condition. An
#     earlier revision of this comment claimed it aborted the run; that was wrong.
#   * Restarting Docker is the wrong remedy regardless. It reclaims no image storage, and
#     four runners execute jobs concurrently here — quitting Docker Desktop mid-run kills
#     unrelated in-flight CI to free nothing. `docker image prune` (steps 3 and 10b) is what
#     actually reclaims.
#
# So: measure, report, and let the prunes do the work.
DOCKER_DF=$(timeout --kill-after=10s 60 docker system df --format '{{.Type}}={{.Size}}' 2>/dev/null || true)
if [ -n "$DOCKER_DF" ]; then
    log "Docker store by type: $(printf '%s' "$DOCKER_DF" | tr '\n' ' ')"
else
    log "WARNING: could not read 'docker system df' within 60s — Docker may be unhealthy"
fi

# 12. Check disk AFTER cleanup
DISK_USED_AFTER=$(disk_used_pct)
log "Disk usage after cleanup: ${DISK_USED_AFTER}% of ${DATA_VOLUME}"

# 13. Alert if still above threshold
if [ "$DISK_USED_AFTER" -gt "$DISK_ALERT_THRESHOLD" ]; then
    log "WARNING: Disk usage still above ${DISK_ALERT_THRESHOLD}%!"
    log "Consider manual intervention or more aggressive cleanup."
fi

log "=== Maintenance complete ==="
echo "---" >> "$LOG_FILE"
