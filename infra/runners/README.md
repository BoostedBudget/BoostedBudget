# Self-hosted GitHub Actions runner maintenance — incident record

**These scripts are host-specific, with hardcoded `/Users/runner` paths. They are a record of
one machine's configuration, not a portable tool. Do not deploy them blind onto another host —
every path, launchd label, and cron entry assumes this exact Mac Mini's layout.**

They are checked into the repo (T3, r5-t-batches, G5-6) because until now they existed **only** on
that one machine, untracked by git, with `.bak-b66a4a6f` files as the sole backup. `git clean
-fdx`, a disk failure, or provisioning a new runner host would have silently lost both the fix and
the diagnosis that produced it. Copied here verbatim — not cleaned up or restructured in this
commit, so this diff is reviewable purely as an import.

- `runner-maintenance.sh` — scheduled (`0 2,14 * * *`) Docker/build-cache/vulnerability-DB cleanup.
- `disk-guard.sh` — scheduled (`0 */4 * * *`) disk-usage alert + escalating cleanup trigger.
- `post-job-cleanup.sh` — per-job cleanup hook run by the runner after every completed job.

## The incident: a 9-month-silent CI outage

Two `crontab` entries meant to catch disk pressure between `runner-maintenance.sh`'s twice-daily
runs never executed a single time, from **2025-10-24 to 2026-08-04**. The emergency cleanup and
the disk-usage alert log it was supposed to write to both stayed silent — not because disk pressure
never happened, but because the entries could never run at all.

### 1. The crontab `%` truncation

The broken entries (preserved in `crontab.bak-b66a4a6f` on the runner host)  embedded `awk
'gsub(/%/,"")'` directly in the crontab command line:

```
0 */4 * * * [ $(df -h / | awk 'NR==2 {gsub(/%/,""); print $5}') -gt 70 ] && /Users/runner/github-runners/runner-maintenance.sh
0 */4 * * * [ $(df -h / | awk 'NR==2 {gsub(/%/,""); print $5}') -gt 70 ] && echo "DISK WARNING: ..." >> ~/github-runners/disk-alerts.log
```

`cron` parses `%` specially: **the first unescaped `%` in a command is replaced with a newline,
and everything after it is fed to the command as stdin**, not treated as more of the command line.
Both lines above have their first `%` inside `gsub(/%/,"")` — so cron truncated each command at
that point and piped the (now-syntactically-broken) remainder in as input. The result, every 4
hours for just over 9 months:

```
/bin/sh: -c: line 0: unexpected EOF while looking for matching `''
```

Quote that exact string — `/bin/sh: -c: line 0: unexpected EOF while looking for matching` — it is
what makes this failure recognisable if it recurs on another host or another cron entry. It arrived
2,600+ times in `/var/mail/runner`, unnoticed because nothing was reading that mailbox (the
count rests on host state no longer inspectable from this repo, not on any imported script).

**Fix:** move all `%`-bearing logic out of the crontab line entirely. `disk-guard.sh` now does its
own threshold check and its own alert-log write internally; the crontab entry is a bare path with
no inline shell metacharacters:

```
0 */4 * * * /Users/runner/github-runners/disk-guard.sh
```

### 2. The APFS volume trap — `df -h /` lies about how full the disk is

macOS's boot volume is a **sealed, read-only System volume** (APFS volume group); the writable data
users actually fill up lives on a **separate** `/System/Volumes/Data` volume that free space is
shared with. `df -h /` reports against the sealed system volume, not the data volume, and macOS's
`df` computes `Capacity` as `used / (used + avail)` **relative to that volume's own numbers** — so
`/` reported **19–31%** used while `/System/Volumes/Data` — the volume actually filling up with
Docker images, build caches, and vulnerability DBs — sat at **79–89%** (these specific readings
rest on host state, not on anything the imported scripts evidence). This is not a rounding
quirk or a different-but-equivalent number: `df -h /` was actively misleading about how close the
host was to running out of space. Any disk-pressure check must read
`/System/Volumes/Data`, not `/`.

### 3. Trivy → Grype: maintenance was pruning the wrong scanner's cache

`runner-maintenance.sh` had a vulnerability-DB cleanup step targeting Trivy's cache. CI had since
switched its scanning tool to Grype, but the maintenance script was never updated to match — so it
kept faithfully pruning an unused Trivy DB while Grype's own DB cache grew unmanaged at
`~/Library/Caches/grype`, reaching **6.7 GB**, entirely outside every path the maintenance script
actually swept (the **6.7 GB** figure itself rests on host state, not on anything the imported
scripts evidence). Fixed by adding Grype's cache directory (with `grype db delete` as the primary
reclaim path, verified against actual freed bytes rather than trusted blindly — see
`purge_grype_cache()` in `runner-maintenance.sh`) alongside Trivy's.

### 4. The `localhost:5432` PG 15 shadowing (an environmental observation about this host, not evidenced by any imported script)

Not part of this incident's timeline, but the same class of "looks fine, silently isn't" failure,
and already flagged in this batch's Part 4: on this machine, a native Homebrew PostgreSQL 15
binds `localhost:5432` on a specific address, and a specific bind beats the Docker Postgres 17
container's wildcard `*:5432` bind. Every host-process connection to `localhost:5432` therefore
reaches the **wrong major version** (15, not 17) — local integration/migration runs against
`localhost:5432` pass without ever touching the container they were meant to validate. Only CI's
own container substantiates a PG17 result; use a non-default port when testing against Postgres
17 from the host.

## Source of truth

These are exact copies of the live scripts as they exist on the runner host today. If the host
copy changes, re-sync manually — nothing automated keeps this checked-in copy and the host in
sync.
