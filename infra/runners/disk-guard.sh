#!/bin/bash
# Disk guard for the self-hosted GitHub Actions runner host.
#
# Runs from cron every 4 hours. Alerts, then invokes maintenance — routinely, or with
# --aggressive when space is critical.
#
# WHY THIS FILE EXISTS — it replaces two inline crontab entries that never once ran.
#
# cron replaces the first UNESCAPED '%' in a command with a newline and feeds everything
# after it to the command as stdin. Both original entries embedded an awk script containing
# a gsub of '%', so cron truncated each command mid-quote and /bin/sh failed with:
#
#     /bin/sh: -c: line 0: unexpected EOF while looking for matching `''
#
# every 4 hours from 2025-10-24 through 2026-08-04 (thousands of such mails in
# /var/mail/runner). Consequences: ~/github-runners/disk-alerts.log was never even
# created, and the emergency cleanup never fired — maintenance.log shows runs only at
# 02:00/14:00 (the one crontab entry containing no '%'), never at the */4 times. The host
# therefore had NO working disk safety net, and on 2026-08-04 CI died with
# "ENOSPC: no space left on device" inside actions/checkout.
#
# Escaping as \% would also work, but keeping every '%' and quote inside a script removes
# the failure mode instead of relying on getting the escaping right again next time.
#
# Deliberately NOT `set -e`: this is a safety net and must still write its alert even when
# maintenance exits non-zero.
set -uo pipefail

DATA_VOLUME="/System/Volumes/Data"
ALERT_LOG="${HOME}/github-runners/disk-alerts.log"
MAINTENANCE="${HOME}/github-runners/runner-maintenance.sh"

# Two independent triggers, because neither alone is sufficient.
#
# Percentage answers "is the disk filling up?", but says nothing about whether the REMAINING
# space can absorb what four concurrent runners are about to write — a full matrix does
# checkouts, node_modules, Docker image builds and a multi-GB Grype DB download, and 11% of
# a 228Gi volume reads as a comfortable "89%" while being far too little.
#
# So also enforce an ABSOLUTE floor in GiB. FREE_MIN is the routine trigger; FREE_CRIT
# escalates to --aggressive. Sized from observed burst consumption (the Grype DB alone was
# 6.7GB) with headroom, not from a round number.
THRESHOLD=70       # percent used -> routine maintenance
CRITICAL=85        # percent used -> aggressive maintenance
FREE_MIN=30        # GiB free below this -> routine maintenance
FREE_CRIT=15       # GiB free below this -> aggressive maintenance

now() { date '+%Y-%m-%d %H:%M:%S'; }

get_disk_pct() {
    df -h "$DATA_VOLUME" 2>/dev/null | awk 'NR==2 {gsub(/%/,""); print $5}'
}

get_free_gb() {
    df -k "$DATA_VOLUME" 2>/dev/null | awk 'NR==2 {printf "%d", $4/1048576}'
}

used=$(get_disk_pct)
free_gb=$(get_free_gb)

# A missing or non-numeric reading is itself worth recording — the previous setup failed
# silently for ten months precisely because nothing ever wrote to a log.
case "${used:-}" in ''|*[!0-9]*) used="" ;; esac
case "${free_gb:-}" in ''|*[!0-9]*) free_gb="" ;; esac
if [ -z "$used" ] || [ -z "$free_gb" ]; then
    echo "[$(now)] disk-guard: could not read ${DATA_VOLUME} (used='${used}' free_gb='${free_gb}')" >> "$ALERT_LOG"
    exit 0
fi

# Inclusive comparisons: -ge / -le, so the NAMED limit is itself a trigger.
# With -gt/-lt, "critical at 85%" actually meant 86%, and "act below 15GiB" meant 14GiB — the
# stated boundary was the one value that did nothing.
mode=""
reason=""
if [ "$used" -ge "$CRITICAL" ] || [ "$free_gb" -le "$FREE_CRIT" ]; then
    mode="--aggressive"
    reason="CRITICAL (${used}% used, ${free_gb}GiB free; limits ${CRITICAL}% / ${FREE_CRIT}GiB)"
elif [ "$used" -ge "$THRESHOLD" ] || [ "$free_gb" -le "$FREE_MIN" ]; then
    reason="warning (${used}% used, ${free_gb}GiB free; limits ${THRESHOLD}% / ${FREE_MIN}GiB)"
fi

[ -z "$reason" ] && exit 0

echo "[$(now)] DISK ${reason} on $(hostname) — running maintenance ${mode:-(routine)}" >> "$ALERT_LOG"

if [ ! -x "$MAINTENANCE" ]; then
    echo "[$(now)] disk-guard: ${MAINTENANCE} missing or not executable" >> "$ALERT_LOG"
    exit 0
fi

# Unquoted on purpose: an empty $mode must expand to NO argument, not an empty one, which
# the script would then compare against "--aggressive".
# shellcheck disable=SC2086
"$MAINTENANCE" $mode >/dev/null 2>&1
rc=$?

# 75 (EX_TEMPFAIL) means maintenance declined because another run held the host-GC lock, so NO WORK
# HAPPENED. Treating that as success was actively dangerous: during a disk emergency an
# --aggressive request could collide with the routine 02:00/14:00 run, be dropped, and be logged as
# "maintenance complete" while the disk kept filling.
#
# Retry budget sized against the plausible holder, not a round number. A lock holder can be the
# post-job hook (up to ~300s of Docker timeouts) or a full maintenance run (longer still), so a
# single 120s wait would usually expire while the holder was still working. Two 180s attempts cover
# ~360s+ and still bound this script well inside its 4-hourly cadence.
LOCK_BUSY_EXIT=75
if [ "$rc" -eq "$LOCK_BUSY_EXIT" ]; then
    attempt=1
    while [ "$rc" -eq "$LOCK_BUSY_EXIT" ] && [ "$attempt" -le 2 ]; do
        echo "[$(now)] disk-guard: maintenance SKIPPED — host-GC lock held; retry ${attempt}/2 in 180s" >> "$ALERT_LOG"
        sleep 180
        # shellcheck disable=SC2086
        "$MAINTENANCE" $mode >/dev/null 2>&1
        rc=$?
        attempt=$((attempt + 1))
    done
    if [ "$rc" -eq "$LOCK_BUSY_EXIT" ]; then
        echo "[$(now)] disk-guard: lock STILL held after 2 retries — requested cleanup ${mode:-(routine)} did NOT run" >> "$ALERT_LOG"
    fi
fi

after_used=$(get_disk_pct)
after_free=$(get_free_gb)

case "$rc" in
    0)  echo "[$(now)] disk-guard: maintenance complete; now ${after_used:-?}% used, ${after_free:-?}GiB free" >> "$ALERT_LOG" ;;
    "$LOCK_BUSY_EXIT")
        echo "[$(now)] disk-guard: maintenance NOT RUN (lock); now ${after_used:-?}% used, ${after_free:-?}GiB free" >> "$ALERT_LOG" ;;
    *)  echo "[$(now)] disk-guard: maintenance exited ${rc}; now ${after_used:-?}% used, ${after_free:-?}GiB free" >> "$ALERT_LOG" ;;
esac

# Still-critical after maintenance means the age filters excluded everything, the lock blocked
# the run, or the remaining usage is genuinely live data. Check BOTH triggers: an earlier
# version tested only free-GiB, so a host sitting above the critical PERCENTAGE with, say,
# 20-29GiB free passed silently — precisely the band this volume occupies when it is in trouble.
if { [ -n "$after_free" ] && [ "$after_free" -le "$FREE_CRIT" ]; } ||
   { [ -n "$after_used" ] && [ "$after_used" -ge "$CRITICAL" ]; }; then
    echo "[$(now)] disk-guard: STILL CRITICAL after maintenance (${after_used:-?}% used, ${after_free:-?}GiB free) — manual intervention likely needed" >> "$ALERT_LOG"
fi
