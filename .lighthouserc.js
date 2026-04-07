const port = process.env.LHCI_PORT || 9222;

module.exports = {
  ci: {
    collect: {
      url: [`http://localhost:${port}/`],
      numberOfRuns: 3,
      startServerCommand: `npx serve apps/web/dist -l ${port} --no-clipboard`,
      startServerReadyPattern: 'Accepting connections',
      // Let LHCI use system Chrome (Google Chrome installed on Mac Mini).
      // Do NOT reuse Playwright's Chromium — it's a patched non-stock build
      // that can produce different scores and breaks after Playwright updates.
      settings: {
        // Desktop emulation — BoostedBudget targets desktop users
        formFactor: 'desktop',
        // Chrome flags for macOS Mac Mini CI stability.
        // --headless (not --headless=new): old headless is proven stable in macOS CI;
        //   --headless=new causes EPERM/WebSocket failures on self-hosted macOS runners.
        // --no-sandbox: required for CI — system Chrome helper processes run with
        //   different permissions, causing kill EPERM without this flag.
        // --disable-dev-shm-usage: prevents memory issues in constrained CI environments.
        chromeFlags: [
          '--headless',
          '--disable-gpu',
          '--no-sandbox',
          '--no-first-run',
          '--disable-extensions',
          '--disable-dev-shm-usage',
        ],
        // No CPU throttling on dedicated Mac Mini CI hardware (avoids amplifying score variance)
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
