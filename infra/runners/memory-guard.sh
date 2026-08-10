#!/bin/bash
# Memory guard for the self-hosted GitHub Actions runner host.
#
# Runs from cron. Alerts when the host is swapping, and removes the two causes it CAN
# remove. Companion to disk-guard.sh — and it exists because disk-guard.sh was the wrong
# axis.
#
# WHY THIS FILE EXISTS — fifteen rounds of CI investigation blamed the tests.
#
# From 2026-08-05 to 2026-08-10, PR #1465's CI failed repeatedly on Frontend Tests and E2E
# Tests with a DIFFERENT set of failing tests almost every run. That disjoint signature was
# read as flakiness, then as "host capacity", and six commits attempted symptomatic fixes to
# the E2E webServer timeout. Every one of those readings was wrong, and the reason nobody
# found the cause is that the only host safety net watched DISK.
#
# What was actually true, measured 2026-08-10 with ZERO CI jobs and ZERO containers running:
#
#     hw.memsize                 16 GB
#     vm.swapusage               8262M used of 9216M   (90% full, 953M free)
#     Swapouts / Swapins         54,773,456 / 52,197,555
#     Pages stored in compressor 1,893,036             (~7.2 GB compressed)
#     Pages free                 3,859                 (~15 MB)
#     load average               22.39                 (with no jobs)
#     mediaanalysisd             61-94% CPU for 8h31m
#     mds_stores (Spotlight)     33.6% CPU
#
# The host was thrashing swap. That produces a UNIFORM 10-50x slowdown across every job
# regardless of what it does, because every process blocks on disk-backed swap I/O. Measured
# on one run: Backend Tests 60.9 min (PASSED), Security Scan 44.1 min, Frontend Tests 46.4
# min, OpenAPI Spec Validation 22.6 min — a job that takes seconds locally.
#
# CPU was never the explanation. Load 14.38 on 10 cores is 1.4x oversubscribed; that cannot
# produce 46x. Swap thrashing can, trivially. And it explains the disjoint failing sets that
# looked like flakiness for fifteen rounds: under a swap storm, WHICH test crosses its
# 5-second waitFor budget is essentially random. The tests were never the variable.
#
# The two causes, neither of which belongs on a CI host:
#   1. mediaanalysisd — the macOS daemon that scans Photos for faces/objects/scenes. This
#      host is also a personal desktop with a real ~/Pictures/Photos Library.photoslibrary.
#   2. Spotlight indexing the runner working directories — 8.6 GB of checkouts, build caches
#      and node_modules being continuously re-indexed, evicting page cache.
#
# ⚠ WHAT THIS GUARD CANNOT DO, stated plainly because disk-guard.sh CAN do the analogue:
# you can delete files to reclaim disk. You cannot un-swap pages. Once the compressor is
# saturated and swap is full, ONLY A REBOOT drains it — stopping the offenders prevents
# further accumulation but does not recover the existing 8.6 GB. So this guard's primary
# product is a LOUD, SPECIFIC alert naming the cause, not a repair.
#
# Deliberately NOT `set -e`: this is a safety net and must still write its alert when any
# individual remediation step fails.
set -uo pipefail

ALERT_LOG="${HOME}/github-runners/memory-alerts.log"
RUNNERS_DIR="${HOME}/github-runners"

# ⚠ THRESHOLD DESIGN, AND THE TWO METRICS THIS GUARD DELIBERATELY DOES NOT USE.
#
# Both were tried first and both are wrong. Recording why, because each looks obviously
# right and each produced a false reading during the investigation that wrote this file:
#
#   * `vm.swapusage used` — NOT a health metric and NOT a recovery metric. macOS does not
#     promptly shrink the swap FILE when pages are freed. Measured here: killing a process
#     with a 1,177 MB footprint moved "used" by 32 MB. A guard thresholding on this reports
#     trouble long after trouble has passed, and reports no improvement after a real fix.
#
#   * Cumulative `Swapouts` from vm_stat — a TOTAL since boot, not a rate. 54,895,720
#     swapouts over 5 days uptime was read as "the host is thrashing right now"; the actual
#     rate at that moment was ZERO. That is the same defect class this repository exists to
#     remove: a counter read as a condition.
#
# What this guard uses instead:
#
#   1. SWAPOUT RATE, sampled over a real interval. This is the only direct evidence of
#      active thrashing, and it is the thing that makes CI jobs 10-50x slow.
#   2. COMPRESSOR SIZE + unused memory. On macOS a tiny "Pages free" is normal — the OS
#      deliberately uses all RAM. What is NOT normal is 7,094 MB parked in the compressor
#      with 90 MB unused, which is the observed steady state on this host: stable at idle,
#      and with no headroom left for a 16-job matrix. That combination is the predictor of
#      the failure, so it warns even when the rate is zero.
SWAPOUT_RATE_WARN=50    # pages/s sustained -> alert
SWAPOUT_RATE_CRIT=500   # pages/s -> actively thrashing; CI will fail on wall-clock budgets
SAMPLE_SECONDS=10       # interval for the rate; short enough for cron, long enough to be real
COMP_MB_WARN=5120       # MB in the compressor -> headroom is gone even if the rate is 0
COMP_MB_CRIT=6656       # -> a full matrix will thrash when it starts
FREE_MB_CRIT=128        # MB unused, as a secondary confirmation only (see note above)

now() { date '+%Y-%m-%d %H:%M:%S'; }

log() { printf '%s  %s\n' "$(now)" "$1" >>"$ALERT_LOG"; }

# --- measurement -------------------------------------------------------------------------

# vm.swapusage prints e.g. "total = 9216.00M  used = 8262.44M  free = 953.56M  (encrypted)".
# Strip the M and take integers; awk over fields is fragile if the format shifts, so match
# on the labels instead of on positions.
# Cumulative swapouts since boot. Only ever used as one end of a RATE — see the threshold
# note above for why the raw total must never be compared against anything.
swapouts_total() { vm_stat 2>/dev/null | awk '/Swapouts/ {gsub(/\./,""); print $2}'; }

# Kept for the alert line only: it is useful context for a human reading the log, and
# actively misleading as a trigger.
swap_used_mb() { sysctl -n vm.swapusage 2>/dev/null | sed -n 's/.*used = \([0-9.]*\)M.*/\1/p' | cut -d. -f1; }

# vm_stat reports pages; page size is 16384 on Apple Silicon, 4096 on Intel. Read it rather
# than assuming — assuming would make this guard's numbers wrong by 4x on the wrong host.
page_size() { vm_stat 2>/dev/null | sed -n 's/.*page size of \([0-9]*\) bytes.*/\1/p'; }
free_mb() {
    local ps pages
    ps="$(page_size)"; pages="$(vm_stat 2>/dev/null | awk '/Pages free/ {gsub(/\./,""); print $3}')"
    [ -n "$ps" ] && [ -n "$pages" ] || { echo 0; return; }
    echo $(( pages * ps / 1048576 ))
}
compressed_mb() {
    local ps pages
    ps="$(page_size)"; pages="$(vm_stat 2>/dev/null | awk '/occupied by compressor/ {gsub(/\./,""); print $5}')"
    [ -n "$ps" ] && [ -n "$pages" ] || { echo 0; return; }
    echo $(( pages * ps / 1048576 ))
}

# --- remediation -------------------------------------------------------------------------

# Re-assert the Spotlight exclusion markers. `_work` itself is never deleted (post-job-cleanup
# removes only workspace-* subdirectories inside it), so these normally persist — but a runner
# re-registration recreates the tree, and a silently missing marker is exactly the kind of
# absence this exercise has learned not to trust.
assert_spotlight_exclusions() {
    local created=0 d
    for d in "$RUNNERS_DIR"/runner-*/_work "$RUNNERS_DIR"/runner-*/_diag; do
        [ -d "$d" ] || continue
        if [ ! -f "$d/.metadata_never_index" ]; then
            touch "$d/.metadata_never_index" 2>/dev/null && created=$((created+1))
        fi
    done
    echo "$created"
}

# mediaanalysisd relaunches on demand; killing it buys back the CPU and the page-cache churn
# until macOS decides to try again. Durable suppression needs sudo and is recorded in
# README.md rather than attempted here — a guard that needs a password cannot run from cron.
stop_media_analysis() {
    local n
    n="$(pgrep -f 'MediaAnalysis.framework.*mediaanalysisd' 2>/dev/null | wc -l | tr -d ' ')"
    if [ "${n:-0}" -gt 0 ]; then
        killall mediaanalysisd 2>/dev/null || true
        killall mediaanalysisd-access 2>/dev/null || true
    fi
    echo "${n:-0}"
}

# --- main --------------------------------------------------------------------------------

main() {
    mkdir -p "$(dirname "$ALERT_LOG")" 2>/dev/null || true

    local free comp rate s1 s2 used
    # Sample the swapout rate over a real interval — the one metric that distinguishes
    # "thrashing now" from "thrashed at some point in the last five days".
    s1="$(swapouts_total)"; sleep "$SAMPLE_SECONDS"; s2="$(swapouts_total)"
    free="$(free_mb)"; comp="$(compressed_mb)"; used="$(swap_used_mb)"

    # Fail closed on unreadable measurements. A guard that silently reports "healthy" because
    # vm_stat changed format is worse than no guard — that is this repository's signature
    # defect, and it has been found in four separate guards on this branch.
    if [ -z "${s1:-}" ] || [ -z "${s2:-}" ] || [ -z "${comp:-}" ] || [ "$(page_size)" = "" ]; then
        log "GUARD BROKEN: could not read vm_stat (swapouts='${s1:-}'/'${s2:-}' comp='${comp:-}' pagesize='$(page_size)'). Not reporting healthy."
        exit 2
    fi

    rate=$(( (s2 - s1) / SAMPLE_SECONDS ))
    [ "$rate" -lt 0 ] && rate=0   # counter reset across a reboot

    local critical=0 warn=0
    [ "$rate" -ge "$SWAPOUT_RATE_CRIT" ] && critical=1
    [ "$comp" -ge "$COMP_MB_CRIT" ] && [ "$free" -le "$FREE_MB_CRIT" ] && critical=1
    [ "$rate" -ge "$SWAPOUT_RATE_WARN" ] && warn=1
    [ "$comp" -ge "$COMP_MB_WARN" ] && warn=1

    if [ "$critical" -eq 0 ] && [ "$warn" -eq 0 ]; then
        # Still re-assert the cheap invariant, silently.
        assert_spotlight_exclusions >/dev/null
        exit 0
    fi

    local sev="WARN"; [ "$critical" -eq 1 ] && sev="CRITICAL"
    log "${sev}: swapout rate ${rate}/s over ${SAMPLE_SECONDS}s, compressor ${comp}M, unused ${free}M (context: swap file ${used:-?}M in use)"

    # Name the offenders. An alert that says "memory is low" sends the next investigator to
    # the tests, which is precisely what happened for fifteen rounds.
    log "  top CPU: $(ps -Ao %cpu,comm -r 2>/dev/null | sed -n '2,4p' | tr '\n' ' ')"
    log "  top RSS: $(ps -Ao rss,comm -m 2>/dev/null | sed -n '2,4p' | tr '\n' ' ')"
    log "  load:    $(sysctl -n vm.loadavg 2>/dev/null) on $(sysctl -n hw.ncpu 2>/dev/null) cores"
    log "  jobs:    $(pgrep -f 'Runner.Worker' 2>/dev/null | wc -l | tr -d ' ') runner workers, $(docker ps -q 2>/dev/null | wc -l | tr -d ' ') containers"

    local killed markers
    killed="$(stop_media_analysis)"
    markers="$(assert_spotlight_exclusions)"
    [ "${killed:-0}" -gt 0 ] && log "  remediated: stopped ${killed} mediaanalysisd process(es)"
    [ "${markers:-0}" -gt 0 ] && log "  remediated: re-created ${markers} Spotlight exclusion marker(s)"

    if [ "$critical" -eq 1 ]; then
        # The honest instruction, corrected twice during the investigation that wrote it.
        #
        # A reboot is the SIMPLEST recovery, not the only one: swapped pages are released when
        # their owning process exits, so restarting the long-lived many-process holders
        # (browser, language servers, the runner listeners themselves) reclaims real memory
        # without bouncing the host. What a reboot uniquely does is drain the compressor.
        #
        # What does NOT work, and must not be attempted in response: raising test timeouts.
        # Six commits on this branch tried that against the E2E webServer budget. The tests
        # were never the variable — see this file's header.
        log "  ⚠ CI jobs will run 10-50x slow and wall-clock assertions will fail at RANDOM."
        log "    Recover by exiting the long-lived memory holders (browser, language servers,"
        log "    idle runner listeners) or by rebooting; a reboot is the only thing that"
        log "    drains the compressor. Do NOT raise test timeouts in response."
    fi

    exit 1
}

main "$@"
