#!/bin/bash
# Post-job cleanup hook
# Runs after EVERY job completes via ACTIONS_RUNNER_HOOK_JOB_COMPLETED

# The runner invokes this hook as `bash -e <path>`, so -e IS active whatever this file says.
#
# Verified in the runner diagnostics: `Arguments: '-e /Users/.../post-job-cleanup.sh'`. An earlier
# comment here claimed the script "deliberately has no set -e" — that was wrong, and it mattered:
# under -e any unguarded failure (a `tee` to a full disk, the final append) exits immediately, so
# the `exit 0` at the bottom was NOT guaranteed to run. Since the hook runs synchronously before
# the job is reported complete, a non-zero exit can be attributed to the JOB — cleanup would then
# turn a passing build red. Disable -e explicitly; every failure below is logged instead.
set +e

# Prepend (do NOT replace) the dirs holding `timeout` and `flock`. flock is keg-only, so it is
# not in /opt/homebrew/bin. Prepending keeps whatever PATH the runner hook already provides, so
# `docker` continues to resolve exactly as before.
export PATH="/opt/homebrew/bin:/opt/homebrew/opt/util-linux/bin:$PATH"

LOG_FILE="${HOME}/github-runners/post-job-cleanup.log"
DATE=$(date '+%Y-%m-%d %H:%M:%S')

# Measure the DATA volume, not `/`.
#
# This script had the same defect as runner-maintenance.sh: `df -h /` reads the sealed
# read-only APFS system volume, whose usage has nothing to do with what CI writes. macOS df
# reports used/(used+avail), and the system volume's own `used` is small, so `/` reported ~19-31%
# while /System/Volumes/Data was at 79-89%. Every disk-triggered branch below (the >70% and >80%
# cache cleanups) was therefore keyed to a number that only crosses its threshold at
# near-exhaustion — which is why the per-job safety net never fired before the 2026-08-04 ENOSPC.
DATA_VOLUME="/System/Volumes/Data"

# Best-effort logging that can never fail the hook. `set +e` above already prevents an abort, but
# this keeps the guarantee local rather than depending on a setting 20 lines up — and the realistic
# trigger is ENOSPC on the very volume this hook is trying to relieve.
log() {
    printf '[%s] %s\n' "$DATE" "$1" 2>/dev/null || true
    { printf '[%s] %s\n' "$DATE" "$1" >> "$LOG_FILE"; } 2>/dev/null || true
    return 0
}

# Skip Docker pruning when host GC already holds the lock.
#
# This hook fires after EVERY job on all four runners, so without coordination up to five
# concurrent prunes hit one Docker daemon (four hooks plus runner-maintenance.sh). Pruning only
# removes unused objects, so the race is wasteful rather than destructive, but it multiplies
# load exactly when the disk is under pressure. Non-blocking: if maintenance holds the lock it
# is already doing this work, so skipping loses nothing.
LOCK_FILE="${HOME}/github-runners/.maintenance.lock"
FLOCK_BIN=""
if command -v flock >/dev/null 2>&1; then
    FLOCK_BIN=$(command -v flock)
elif [ -x /opt/homebrew/opt/util-linux/bin/flock ]; then
    FLOCK_BIN=/opt/homebrew/opt/util-linux/bin/flock
fi

docker_gc_permitted() {
    [ -z "$FLOCK_BIN" ] && return 0          # no flock: behave as before rather than skip work
    exec 8>"$LOCK_FILE" 2>/dev/null || return 0
    "$FLOCK_BIN" -n 8 2>/dev/null            # holds until this script exits
}

log "Starting post-job cleanup for job: ${GITHUB_JOB:-unknown}"

# 1-4. Docker pruning — bounded, and skipped while host GC holds the lock.
#
# Every call is wrapped in `timeout --kill-after`. These were unbounded, and this hook runs
# BEFORE the runner reports the job finished, so a wedged Docker daemon stalled job completion
# and held the runner slot. `docker system df` was measured hanging over 300s on this host, so
# that is not hypothetical. --kill-after matters because plain timeout only sends TERM.
if docker_gc_permitted; then
    log "Pruning Docker build cache..."
    timeout --kill-after=10s 60 docker builder prune -f 2>&1 | tee -a "$LOG_FILE" || true

    log "Pruning stopped containers..."
    timeout --kill-after=10s 60 docker container prune -f 2>&1 | tee -a "$LOG_FILE" || true

    log "Pruning unused Docker images..."
    timeout --kill-after=10s 120 docker image prune -af --filter "until=24h" 2>&1 | tee -a "$LOG_FILE" || true

    log "Pruning unused volumes..."
    timeout --kill-after=10s 60 docker volume prune -f 2>&1 | tee -a "$LOG_FILE" || true
else
    log "Skipping Docker prune — the host GC lock is held (by maintenance or a sibling hook); that holder is doing this work"
fi

# 5. Clean workspace temp files using correct env var
if [ -n "$GITHUB_WORKSPACE" ]; then
    log "Cleaning workspace temp files in: $GITHUB_WORKSPACE"
    find "$GITHUB_WORKSPACE" -type f -name "*.tmp" -delete 2>/dev/null || true
    # -prune stops find recursing into directories it has just rm -rf'd (which otherwise emits
    # a "No such file or directory" per descendant); `-exec ... +` batches instead of forking
    # rm per match. Same fix as runner-maintenance.sh's workspace sweep.
    find "$GITHUB_WORKSPACE" -type d -name "workspace-*" -prune -exec rm -rf -- {} + 2>/dev/null || true
fi

# 6. Clean vulnerability-scanner caches when the DATA volume is tight.
#
# Two fixes here. First, the reading is taken from $DATA_VOLUME instead of `/` (see the header):
# keyed to `/`, neither the 70% nor the 80% branch could fire until the disk was nearly full.
# Second, this cleaned ONLY Trivy — but CI replaced Trivy with Grype (the
# "Docker Image Security Scan (Grype)" job in .github/workflows/test.yml). Grype caches under
# ~/Library/Caches/grype, which nothing in this hook ever touched, and it was measured at 6.7GB.
# So the branch that was supposed to save the host cleaned a scanner CI no longer runs, using a
# threshold it could not reach. Trivy handling is retained — harmless if absent.
DISK_USED=$(df -h "$DATA_VOLUME" 2>/dev/null | awk 'NR==2 {gsub(/%/,""); print $5}')
case "${DISK_USED:-}" in ''|*[!0-9]*) DISK_USED=0 ;; esac

if [ "$DISK_USED" -ge 70 ]; then
    log "DATA volume at ${DISK_USED}%, cleaning scanner caches..."
    if [ -n "$GITHUB_WORKSPACE" ]; then
        rm -rf "${GITHUB_WORKSPACE}/.cache/trivy/db/trivy.db" 2>/dev/null || true
        rm -rf "${GITHUB_WORKSPACE}/.cache/trivy/java-db/trivy-java.db" 2>/dev/null || true
        log "Workspace Trivy caches cleaned (no-op if Trivy is unused)"
    fi
    if [ "$DISK_USED" -ge 80 ]; then
        log "CRITICAL: DATA volume at ${DISK_USED}%, clearing scanner caches host-wide..."
        for runner in "${HOME}/github-runners/runner-"{1,2,3,4}; do
            find "$runner" -path "*/.cache/trivy/*" -type f \( -name "trivy.db" -o -name "trivy-java.db" \) -delete 2>/dev/null || true
        done
        # The Grype DB is regenerated on the next scan, but it is SHARED: this hook fires as its own
        # job ends while three sibling runners may still be mid-scan.
        #
        # Count workers WITHOUT `pgrep -c` — macOS /usr/bin/pgrep has no -c and exits with a
        # usage error. The previous `pgrep -cf ... || echo 0` therefore turned that error into a
        # count of ZERO, the `-le 1` test passed, and the shared Grype cache was deleted even with
        # sibling jobs mid-scan. That is the same fail-open mistake this script's maintenance
        # counterpart already fixed, reintroduced here. Verified: `pgrep -cf` prints usage and the
        # expression evaluated to 0 => DELETE.
        #
        # Fail CLOSED on every ambiguity. This hook runs synchronously while its OWN job's worker
        # still exists, so a count of 0 is not "idle" — it means the probe did not work, and it is
        # treated as busy. Only "exactly one worker (mine)" permits touching the shared cache.
        worker_pids=$(pgrep -f "Runner.Worker" 2>/dev/null)
        pgrep_rc=$?
        worker_count=$(printf '%s' "$worker_pids" | grep -c '[0-9]')
        grype_safe=0
        if [ "$pgrep_rc" -gt 1 ]; then
            log "Grype gate: pgrep failed (exit ${pgrep_rc}) — treating host as BUSY"
        elif [ "$worker_count" -eq 0 ]; then
            log "Grype gate: found no Runner.Worker at all (own worker expected) — treating host as BUSY"
        elif [ "$worker_count" -gt 1 ]; then
            log "Grype gate: ${worker_count} workers running — sibling jobs may be scanning, leaving cache"
        else
            grype_safe=1
        fi

        if [ "$grype_safe" -eq 1 ] && [ -d "${HOME}/Library/Caches/grype" ]; then
            log "Clearing Grype DB cache ($(du -sm "${HOME}/Library/Caches/grype" 2>/dev/null | awk '{print $1}')MB, re-downloaded on next scan)"
            rm -rf "${HOME}/Library/Caches/grype" 2>/dev/null || true
        else
            log "Grype DB cache left alone — sibling jobs may be scanning"
        fi
    fi
fi

# 7. Report disk usage — the DATA volume, so the log means something.
log "Disk usage after cleanup:"
df -h "$DATA_VOLUME" 2>&1 | tee -a "$LOG_FILE"

log "Post-job cleanup completed"
echo "---" >> "$LOG_FILE"

# Always succeed. This runs as ACTIONS_RUNNER_HOOK_JOB_COMPLETED, where a non-zero exit can be
# reported against the job itself — a cleanup hook must never turn a passing job red. Cleanup is
# best-effort by design and every failure above is already logged.
exit 0
