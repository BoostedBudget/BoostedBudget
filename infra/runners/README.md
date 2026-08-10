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

## The second incident: fifteen rounds of CI failures blamed on the tests (2026-08-05 → 08-10)

`disk-guard.sh` came out of the first incident. **It was the wrong axis for the second one**, and
nothing watched memory, so the cause went unfound for five days across fifteen review rounds.

**Symptom.** PR #1465's CI failed repeatedly on Frontend Tests and E2E Tests with a *different set
of failing tests almost every run*. That disjoint signature was read as flakiness, then as "host
capacity", and **six commits attempted symptomatic fixes to the E2E `webServer` timeout**.

**What was actually measured** (run 31353967309, all on one 16 GB / 10-core M4 Mac Mini):

| job | duration | outcome |
|---|---|---|
| Backend Tests | **60.9 min** | **passed** (≈1 min locally) |
| Frontend Tests | 46.4 min | failed |
| Security Scan (backend) | 44.1 min | passed |
| Backend Integration Tests | 28.8 min | cancelled |
| OpenAPI Spec Validation | **22.6 min** | passed (seconds locally) |

**Every job was 10–50× slower than local, including the ones that passed.** That is the fact
fifteen rounds never established, and it rules out test logic immediately: no frontend defect makes
`OpenAPI Spec Validation` take 22 minutes.

**Cause.** Memory is permanently saturated on this host: **15 GB of 16 GB used, ~7 GB parked in the
compressor, ~90 MB unused**, at *idle*. Contributors: four runner listeners, plus two macOS desktop
services that have no business on a CI box — `mediaanalysisd` (Photos face/object analysis, observed
at 61–94% CPU for 8.5 h against a real `~/Pictures/Photos Library.photoslibrary`) and Spotlight
indexing 8.6 GB of runner working directories. At idle that state is stable. **When a 16-job matrix
starts, four runners doing Go builds, Docker, bun and vite demand far more than the remaining
headroom**, the machine goes into heavy compression, and every process blocks. Which test then
crosses its 5-second `waitFor` budget is essentially random — hence the disjoint failing sets.

**⚠ Three claims made during this investigation were wrong, and each is the reason a metric was
rejected. They are recorded because each looks obviously right:**

1. *"The host is thrashing swap right now."* **False.** 54,895,720 swapouts is a **cumulative total
   since boot** (5 days uptime); the measured **rate** at that moment was **0/s**. A counter read as
   a condition — the exact defect class this repository exists to remove.
2. *"Only a reboot can fix it."* **Overstated.** Swapped pages are released when their owning
   process exits, so restarting long-lived many-process holders reclaims real memory. A reboot is
   uniquely what drains the *compressor*.
3. *"`vm.swapusage used` measures recovery."* **False.** macOS does not promptly shrink the swap
   file. Killing a process with a 1,177 MB footprint moved "used" by **32 MB**.

Thermal throttling was also tested and **refuted** — `pmset -g therm` records no thermal or
performance warning, on AC power.

**Fixes applied.** `.metadata_never_index` markers in every runner `_work`/`_diag` directory
(reversible: delete the file); `mediaanalysisd` stopped — ⚠ **it relaunches on demand, observed three
times within minutes**, so durable suppression needs
`sudo launchctl disable system/com.apple.mediaanalysisd`; and `memory-guard.sh`, which thresholds on
the **swapout rate** plus compressor occupancy rather than on either rejected metric, and fails
closed when `vm_stat` is unreadable.

⚠ **What must not be done in response to this class of failure: raising test timeouts.** Six commits
tried it. The tests were never the variable.

## Source of truth

These are exact copies of the live scripts as they exist on the runner host today. If the host
copy changes, re-sync manually — nothing automated keeps this checked-in copy and the host in
sync.
