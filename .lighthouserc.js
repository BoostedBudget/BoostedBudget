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
    assert: {
      assertions: {
        'categories:performance': ['error', { minScore: 0.8 }],
        'categories:accessibility': ['error', { minScore: 0.95 }],
        'categories:best-practices': ['error', { minScore: 0.9 }],
        'categories:seo': ['error', { minScore: 0.95 }],
      },
    },
    upload: {
      // Filesystem target — ephemeral, cleaned up with workspace. No server, no artifacts.
      target: 'filesystem',
      outputDir: '/tmp/lhci-reports',
    },
  },
};
