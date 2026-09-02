#!/usr/bin/env node
/**
 * Find available ports for E2E testing to avoid conflicts in CI with concurrent runners.
 * Outputs environment variables that can be sourced in CI scripts.
 *
 * Usage:
 *   node scripts/find-available-ports.js
 *
 * Sets:
 *   E2E_FRONTEND_PORT  - Dynamic port for Vite dev server
 *   E2E_BACKEND_PORT   - Dynamic port for backend server
 *   E2E_POSTGRES_PORT  - Dynamic port for PostgreSQL container
 *   E2E_PROD_PORT      - Dynamic port for Vite production preview (prod-bundle smoke tests)
 *   E2E_METRICS_PORT   - Dynamic port for the backend's internal metrics listener
 *                        (INTERNAL_METRICS_PORT; hardcoded 9091 collides across parallel
 *                        E2E runs/CI runners — Issue #1514)
 *
 * Note: this script does NOT emit LHCI_PORT. Lighthouse CI (`.github/workflows/test.yml`,
 * the `lighthouse-audit` job) sets its own fixed LHCI_PORT and never invokes this script, so an
 * LHCI_PORT emitted here
 * would be dead output — verified by checking that run-e2e-with-ports.sh only exports
 * variables matching `^E2E_` (see its `grep -E "^E2E_"`) and that no other caller reads
 * this script's stdout for LHCI_PORT.
 *
 * Port layout (Issue #1514): one contiguous 600-port block per job, carved into five
 * 100-port per-service sub-bands, sized to sit below the host's ephemeral port floor:
 *   block    = 15000 + (runId % 50) * 600
 *   frontend = block +   0 .. +  99
 *   backend  = block + 100 .. + 199
 *   postgres = block + 200 .. + 299
 *   prod     = block + 300 .. + 399
 *   metrics  = block + 400 .. + 499
 * Max possible port: 15000 + 49*600 + 599 = 44999.
 *
 * ⚠ THAT CEILING IS ONLY BELOW THE FLOOR ON macOS. #1598:
 *   macOS: net.inet.ip.portrange.first = 49152, so 44999 is safely below it as shipped. This
 *          was the only platform that mattered when the layout was designed.
 *   Linux: net.ipv4.ip_local_port_range defaults to 32768, which OVERLAPS this block from 32768
 *          to 44999 — so the kernel can hand an outbound socket a port this script is about to
 *          hand a server, producing an intermittent, load-dependent EADDRINUSE that reads as
 *          test flake rather than as a port-allocation bug.
 *
 * In CI that overlap is currently bounded rather than eliminated, by two properties of the
 * ephemeral runner: there is exactly one job per VM, and the databases are GitHub Actions
 * `services:` containers on Actions-mapped ports, so nothing else publishes into this range.
 * Do NOT reuse this allocator on a busy Linux host without either raising the kernel floor
 * (`sysctl -w net.ipv4.ip_local_port_range="49152 60999"`) or lowering the block ceiling below
 * 32768.
 *
 * Blocks are unique per *offset* (runId % 50), not per run: runs 50 apart (e.g. 7 and 57)
 * share the same block. Within a shared block, findAvailablePort's in-band scan picks
 * whichever ports are actually free, and run-e2e-with-ports.sh's check_port_free preflight
 * guards immediately before the backend/frontend/prod servers launch. A scan-to-bind TOCTOU
 * race still remains — this reduces collision likelihood, it does not eliminate it.
 */

import net from 'net';

/**
 * Find an available port in the given range
 * @param {number} startPort - Start of port range
 * @param {number} endPort - End of port range
 * @returns {Promise<number>} - Available port number
 */
function findAvailablePort(startPort, endPort) {
  return new Promise((resolve, reject) => {
    const tryPort = (port) => {
      if (port > endPort) {
        reject(new Error(`No available ports in range ${startPort}-${endPort}`));
        return;
      }

      const server = net.createServer();

      server.once('error', (err) => {
        if (err.code === 'EADDRINUSE') {
          // Port in use, try next port
          tryPort(port + 1);
        } else {
          reject(err);
        }
      });

      server.once('listening', () => {
        // Port is available
        server.close(() => {
          resolve(port);
        });
      });

      server.listen(port, '127.0.0.1');
    };

    tryPort(startPort);
  });
}

async function main() {
  try {
    // Derive a unique 600-port block from GITHUB_RUN_ID to prevent conflicts, carved into
    // five 100-port per-service sub-bands. See the header comment for the full rationale
    // (ephemeral-port-floor avoidance, and the runId % 50 residual).
    const runId = process.env.GITHUB_RUN_ID || '0';
    const block = 15000 + (parseInt(runId) % 50) * 600;

    const frontendStart = block + 0;
    const frontendEnd = frontendStart + 99;
    const backendStart = block + 100;
    const backendEnd = backendStart + 99;
    const postgresStart = block + 200;
    const postgresEnd = postgresStart + 99;
    const prodStart = block + 300;
    const prodEnd = prodStart + 99;
    const metricsStart = block + 400;
    const metricsEnd = metricsStart + 99;

    // Find available ports within this job's dedicated range
    const frontendPort = await findAvailablePort(frontendStart, frontendEnd);
    const backendPort = await findAvailablePort(backendStart, backendEnd);
    const postgresPort = await findAvailablePort(postgresStart, postgresEnd);
    const prodPort = await findAvailablePort(prodStart, prodEnd);
    const metricsPort = await findAvailablePort(metricsStart, metricsEnd);

    // Output as environment variables for CI
    console.log(`E2E_FRONTEND_PORT=${frontendPort}`);
    console.log(`E2E_BACKEND_PORT=${backendPort}`);
    console.log(`E2E_POSTGRES_PORT=${postgresPort}`);
    console.log(`E2E_PROD_PORT=${prodPort}`);
    console.log(`E2E_METRICS_PORT=${metricsPort}`);

    // Also output JSON for programmatic use
    console.error(JSON.stringify({
      frontend: frontendPort,
      backend: backendPort,
      postgres: postgresPort,
      prod: prodPort,
      metrics: metricsPort,
      range: `frontend=${frontendStart}-${frontendEnd}, backend=${backendStart}-${backendEnd}, postgres=${postgresStart}-${postgresEnd}, prod=${prodStart}-${prodEnd}, metrics=${metricsStart}-${metricsEnd}`
    }));
  } catch (error) {
    console.error('Error finding available ports:', error.message);
    process.exit(1);
  }
}

main();
