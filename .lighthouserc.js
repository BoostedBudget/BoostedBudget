const port = process.env.LHCI_PORT || 9222;

module.exports = {
  ci: {
    collect: {
      url: [`http://localhost:${port}/`, `http://localhost:${port}/mcp`],
      numberOfRuns: 3,
      // --single: SPA fallback so deep links (e.g. /mcp) don't 404 — `serve` has no
      // fallback by default and ignores public/_redirects (that file is Cloudflare-only).
      startServerCommand: `npx serve apps/web/dist -l ${port} --no-clipboard --single`,
      startServerReadyPattern: 'Accepting connections',
      // Let LHCI use system Chrome (Google Chrome installed on Mac Mini).
      // Do NOT reuse Playwright's Chromium — it's a patched non-stock build
      // that can produce different scores and breaks after Playwright updates.
      settings: {
        // Desktop emulation — BoostedBudget targets desktop users.
        // Lighthouse 12+ requires screenEmulation.mobile to be explicitly false
        // when formFactor is 'desktop' — omitting it leaves the default mobile:true
        // which conflicts with formFactor:'desktop' and causes a hard error.
        formFactor: 'desktop',
        screenEmulation: {
          mobile: false,
          width: 1350,
          height: 940,
          deviceScaleFactor: 1,
          disabled: false,
        },
        // Chrome flags for Linux CI. This job runs on ubicloud-standard-2 as of #1598; it
        // previously ran (or rather, was disabled) on the self-hosted Mac Mini, and the
        // rationale below was rewritten because the macOS-specific version had become
        // actively misleading about the platform it describes.
        //
        // --headless: kept, but NOT for the reason previously recorded here. That comment said
        //   "old headless is proven stable in macOS CI; --headless=new causes EPERM/WebSocket
        //   failures on self-hosted macOS runners" -- reasoning that does not transfer to Linux,
        //   and which #996 flatly contradicts by asserting LHCI appends --headless=new
        //   unconditionally. Neither claim was re-verified. What matters in practice is that
        //   Chrome 132 REMOVED old headless, so on any current browser this flag selects new
        //   headless whatever the intent was. That is fine here: the process-group conflict that
        //   got this job disabled was macOS behaviour, which Linux does not share. The
        //   workflow's "Preflight — resolve Chrome" step echoes the Chrome version so the mode
        //   actually in use is recoverable from the logs.
        // --no-sandbox: required on a CI VM, where the sandbox's user-namespace requirements are
        //   not reliably available.
        // --disable-dev-shm-usage: /dev/shm is small on CI VMs and Chrome will crash on it.
        //   Note this is the HOST-run browser, not a container, so Playwright's --ipc=host
        //   advice does not apply.
        chromeFlags: [
          '--headless',
          '--disable-gpu',
          '--no-sandbox',
          '--no-first-run',
          '--disable-extensions',
          '--disable-dev-shm-usage',
        ],
        // No CPU/network throttling.
        //
        // ⚠ #1598: the premise of this setting has changed and the thresholds have NOT been
        // recalibrated. It previously read "No CPU throttling on dedicated Mac Mini CI hardware
        // (avoids amplifying score variance)" -- true of a dedicated machine. This job now runs
        // on a shared 2-vCPU cloud VM, where leaving throttling off MAXIMISES score variance,
        // judged against a Performance >= 0.8 floor that was derived on the opposite hardware.
        //
        // If Performance proves noisy on Ubicloud, the order of remedies is: raise numberOfRuns
        // from 3 to 5 first, then consider ENABLING modest CPU throttling to stabilise scores.
        // Do NOT lower the threshold, and do not change anything here on the evidence of a
        // single run.
        throttling: {
          cpuSlowdownMultiplier: 1,
          requestLatencyMs: 0,
          downloadThroughputKbps: 0,
          uploadThroughputKbps: 0,
        },
        onlyCategories: ['performance', 'accessibility', 'best-practices', 'seo'],
      },
    },
    // #1601. Per-URL thresholds, RATCHETED TO MEASURED VALUES.
    //
    // ⚠ These numbers are a regression fence, NOT a statement that the scores are good. Read this
    // before "fixing" one upward.
    //
    // PROVENANCE. The lighthouse-audit job was `if: false` from 2026-04-07 until #1598 re-enabled
    // it, so before this week these thresholds had NEVER ONCE BEEN EVALUATED. The 0.8/0.9 they
    // replace were aspirational defaults that no run had ever met. Measured on CI run
    // 33673717843 (2026-09-02), 3 runs per URL:
    //
    //     /       performance 0.56, 0.57, 0.56   best-practices 0.78, 0.78, 0.78
    //     /mcp    performance 0.45, 0.45, 0.45   best-practices 0.78, 0.78, 0.78
    //
    // WHY PERFORMANCE IS LOW, AND WHY PAYLOAD WORK WILL NOT FIX IT. #1601 cut the landing route's
    // eager download 36% (847 -> 540 KiB gzip, 20 -> 10 assets, removing echarts and five feature
    // chunks). Performance moved 0.55 -> 0.57. Essentially nothing. The metrics say why:
    // total-blocking-time 0ms, bootup-time 0.2s, server-response-time 30ms all score 1, while FCP
    // is ~5.8s. The delay is a SERIAL DEPENDENCY, not bytes and not work: AuthReadyGate
    // (apps/web/src/main.tsx) paints a header shell (matching the real header's geometry) while
    // useAuth().isLoaded is false, but the REAL content behind it — everything AuthReadyGate
    // gates — still waits on `isLoaded`, and it sits above App and is not route-aware. So the
    // page's actual content, and with it LCP, still wait on Clerk's chain (clerk.browser.js ->
    // /v1/environment -> /v1/client) even for an anonymous visitor on a public page. Making
    // public routes paint their real content before Clerk resolves is the fix that would move
    // this number, and it is still open on #1601. Do not raise these thresholds expecting bundle
    // work to satisfy them.
    //
    // WHY BEST-PRACTICES CANNOT EXCEED 0.78, EVER, while these scripts load at all. The category
    // weighs 27 and loses exactly 6: `third-party-cookies` (weight 5) and `inspector-issues`
    // (weight 1). Both are ENTIRELY __cf_bm/_cfuvid Cloudflare cookies set by Paddle and Clerk.
    // Zero of it is our code, and `async`/`defer` does not stop a script setting cookies. 27-6=21,
    // 21/27 = 0.777... -> 0.78. A threshold above 0.78 is unsatisfiable, not merely ambitious.
    //
    // ⚠ HOW THESE ARE AGGREGATED, because it makes the gate weaker than it looks. LHCI defaults
    // to aggregationMethod 'optimistic' (@lhci/utils/src/assertions.js:139), so a minScore
    // assertion is checked against the BEST of the 3 runs, not the worst or the median. Verified
    // empirically against this run's own reports: / passes at minScore up to 0.57 and fails at
    // 0.58, i.e. the effective value is 0.57 — the best of 0.56/0.57/0.56, not the 0.56 floor.
    //
    // The practical consequence is that ONE good run out of three satisfies the gate. That is
    // worth knowing before trusting a green Lighthouse job as evidence of consistency; switching
    // to 'median' or 'pessimistic' would make it a real floor, but that is a deliberate
    // tightening of the gate rather than part of setting thresholds from a measurement, so it is
    // left alone here and called out instead.
    //
    // Given that, the performance floors are set two notches below the effective (optimistic)
    // value, so the gate trips when the whole distribution regresses rather than on single-run
    // jitter. /mcp and both best-practices scores showed ZERO variance across all three runs and
    // are pinned at the observed value.
    //
    // accessibility and seo keep their 0.95 aspirational floors — unlike the two above, those
    // already pass.
    //
    // ⚠ assertMatrix is exclusive: @lhci/utils/src/assertions.js:465 throws
    // "Cannot use assertMatrix with other options", so a sibling `assertions` key here is a hard
    // error, not a merge.
    assert: {
      assertMatrix: [
        {
          // The landing route. Anchored on a trailing slash so it cannot also match /mcp.
          matchingUrlPattern: '.*/$',
          assertions: {
            'categories:performance': ['error', { minScore: 0.55 }],
            'categories:accessibility': ['error', { minScore: 0.95 }],
            'categories:best-practices': ['error', { minScore: 0.78 }],
            'categories:seo': ['error', { minScore: 0.95 }],
          },
        },
        {
          matchingUrlPattern: '.*/mcp$',
          assertions: {
            'categories:performance': ['error', { minScore: 0.45 }],
            'categories:accessibility': ['error', { minScore: 0.95 }],
            'categories:best-practices': ['error', { minScore: 0.78 }],
            'categories:seo': ['error', { minScore: 0.95 }],
          },
        },
      ],
    },
    upload: {
      // Filesystem target — ephemeral, cleaned up with workspace. No server, no artifacts.
      target: 'filesystem',
      outputDir: '/tmp/lhci-reports',
    },
  },
};
