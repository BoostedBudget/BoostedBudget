#!/usr/bin/env node
/**
 * Find available ports for E2E testing to avoid conflicts in CI with concurrent runners.
 * Outputs environment variables that can be sourced in CI scripts.
 *
 * Usage:
 *   node scripts/find-available-ports.js
 *
 * Sets:
 *   E2E_FRONTEND_PORT - Dynamic port for Vite dev server
 *   E2E_BACKEND_PORT  - Dynamic port for backend server
 */

import net from 'net';

/**
 * Find an available port in the given range, excluding reserved ports
 * @param {number} startPort - Start of port range
 * @param {number} endPort - End of port range
 * @param {number[]} excludePorts - Ports to skip (reserved for staging)
 * @returns {Promise<number>} - Available port number
 */
function findAvailablePort(startPort, endPort, excludePorts = []) {
  return new Promise((resolve, reject) => {
    const tryPort = (port) => {
      if (port > endPort) {
        reject(new Error(`No available ports in range ${startPort}-${endPort}`));
        return;
      }

      // Skip excluded ports (reserved for staging)
      if (excludePorts.includes(port)) {
        tryPort(port + 1);
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
    // Find ports in different ranges to avoid conflicts
    // Frontend: 3000-3999 (excluding 3000 - reserved for staging)
    // Backend: 8000-8999 (excluding 8080 - reserved for staging)
    const frontendPort = await findAvailablePort(3000, 3999, [3000]);
    const backendPort = await findAvailablePort(8000, 8999, [8080]);

    // Output as environment variables for CI
    console.log(`E2E_FRONTEND_PORT=${frontendPort}`);
    console.log(`E2E_BACKEND_PORT=${backendPort}`);

    // Also output JSON for programmatic use
    console.error(JSON.stringify({
      frontend: frontendPort,
      backend: backendPort
    }));
  } catch (error) {
    console.error('Error finding available ports:', error.message);
    process.exit(1);
  }
}

main();
