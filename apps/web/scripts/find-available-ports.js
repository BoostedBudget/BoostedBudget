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
 *   LHCI_PORT          - Dynamic port for Lighthouse CI static file server
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
    // Derive unique port range from GITHUB_RUN_ID to prevent conflicts
    // between concurrent CI jobs on shared self-hosted runner
    const runId = process.env.GITHUB_RUN_ID || '0';
    const portOffset = (parseInt(runId) % 50) * 100;

    // Each job gets an isolated 100-port range:
    // - Frontend: 30000-34900 (50 ranges of 100 ports)
    // - Backend: 35000-39900 (50 ranges of 100 ports)
    // - Postgres: 40000-44900 (50 ranges of 100 ports)
    // - LHCI:     45000-49900 (50 ranges of 100 ports)
    // This supports up to 50 concurrent jobs with zero overlap
    const frontendStart = 30000 + portOffset;
    const frontendEnd = frontendStart + 99;
    const backendStart = 35000 + portOffset;
    const backendEnd = backendStart + 99;
    const postgresStart = 40000 + portOffset;
    const postgresEnd = postgresStart + 99;
    const lhciStart = 45000 + portOffset;
    const lhciEnd = lhciStart + 99;

    // Find available ports within this job's dedicated range
    const frontendPort = await findAvailablePort(frontendStart, frontendEnd);
    const backendPort = await findAvailablePort(backendStart, backendEnd);
    const postgresPort = await findAvailablePort(postgresStart, postgresEnd);
    const lhciPort = await findAvailablePort(lhciStart, lhciEnd);

    // Output as environment variables for CI
    console.log(`E2E_FRONTEND_PORT=${frontendPort}`);
    console.log(`E2E_BACKEND_PORT=${backendPort}`);
    console.log(`E2E_POSTGRES_PORT=${postgresPort}`);
    console.log(`LHCI_PORT=${lhciPort}`);

    // Also output JSON for programmatic use
    console.error(JSON.stringify({
      frontend: frontendPort,
      backend: backendPort,
      postgres: postgresPort,
      lhci: lhciPort,
      range: `frontend=${frontendStart}-${frontendEnd}, backend=${backendStart}-${backendEnd}, postgres=${postgresStart}-${postgresEnd}, lhci=${lhciStart}-${lhciEnd}`
    }));
  } catch (error) {
    console.error('Error finding available ports:', error.message);
    process.exit(1);
  }
}

main();
