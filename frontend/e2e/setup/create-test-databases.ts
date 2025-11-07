/**
 * Global Setup Script for E2E Scenarios
 *
 * Creates isolated test databases for each Playwright worker to prevent test pollution.
 * Each worker gets its own database (boostedbudget_test_worker_0, _1, _2, _3).
 *
 * Issue #331: E2E Test Scenarios Framework
 */

import { execSync } from 'child_process';
import { Pool } from 'pg';

const WORKER_COUNT = 4; // Matches Playwright workers config
const BASE_DB_URL = process.env.TEST_DATABASE_URL || 'postgres://testuser:testpass@localhost:5433';
const MAIN_DB_NAME = 'boostedbudget_test';

/**
 * Parse database URL to extract components
 */
function parseDatabaseUrl(url: string): {
  protocol: string;
  user: string;
  password: string;
  host: string;
  port: string;
  database: string;
} {
  const match = url.match(/^(postgres:\/\/)([^:]+):([^@]+)@([^:]+):(\d+)\/(.+?)(\?.*)?$/);
  if (!match) {
    throw new Error(`Invalid database URL format: ${url}`);
  }

  return {
    protocol: match[1],
    user: match[2],
    password: match[3],
    host: match[4],
    port: match[5],
    database: match[6],
  };
}

/**
 * Create a database if it doesn't exist
 */
async function createDatabase(adminPool: Pool, dbName: string): Promise<void> {
  try {
    // Check if database exists
    const result = await adminPool.query(
      `SELECT 1 FROM pg_database WHERE datname = $1`,
      [dbName]
    );

    if (result.rows.length === 0) {
      // Database doesn't exist, create it
      console.log(`  Creating database: ${dbName}`);
      await adminPool.query(`CREATE DATABASE ${dbName}`);
      console.log(`  ✓ Database ${dbName} created`);
    } else {
      console.log(`  ✓ Database ${dbName} already exists`);
    }
  } catch (error) {
    console.error(`  ✗ Failed to create database ${dbName}:`, error);
    throw error;
  }
}

/**
 * Run Atlas migrations on a database
 */
async function runMigrations(dbUrl: string, dbName: string): Promise<void> {
  try {
    console.log(`  Running migrations on: ${dbName}`);

    // Construct full database URL
    const parsed = parseDatabaseUrl(dbUrl);
    const fullUrl = `${parsed.protocol}${parsed.user}:${parsed.password}@${parsed.host}:${parsed.port}/${dbName}?sslmode=disable`;

    // Run Atlas migrations
    // Note: This assumes atlas is available in PATH and migrations are in backend/migrations
    const migrationDir = 'file://../../backend/migrations';
    const command = `atlas migrate apply --dir ${migrationDir} --url "${fullUrl}"`;

    execSync(command, { stdio: 'inherit' });
    console.log(`  ✓ Migrations completed for ${dbName}`);
  } catch (error) {
    console.error(`  ✗ Failed to run migrations on ${dbName}:`, error);
    throw error;
  }
}

/**
 * Main setup function
 */
async function globalSetup(): Promise<void> {
  console.log('\n🚀 Setting up E2E test environment...\n');

  const parsed = parseDatabaseUrl(BASE_DB_URL);

  // Connect to postgres database (always exists) to create other databases
  const adminUrl = `${parsed.protocol}${parsed.user}:${parsed.password}@${parsed.host}:${parsed.port}/postgres`;
  const adminPool = new Pool({
    connectionString: adminUrl,
  });

  try {
    console.log('📦 Creating worker databases...\n');

    // Create main test database
    await createDatabase(adminPool, MAIN_DB_NAME);

    // Create worker-specific databases
    for (let i = 0; i < WORKER_COUNT; i++) {
      const workerDbName = `boostedbudget_test_worker_${i}`;
      await createDatabase(adminPool, workerDbName);

      // Set environment variable for this worker
      process.env[`TEST_DATABASE_URL_${i}`] =
        `${parsed.protocol}${parsed.user}:${parsed.password}@${parsed.host}:${parsed.port}/${workerDbName}?sslmode=disable`;
    }

    console.log('\n🔧 Running migrations on all databases...\n');

    // Run migrations on main test database
    await runMigrations(BASE_DB_URL, MAIN_DB_NAME);

    // Run migrations on each worker database
    for (let i = 0; i < WORKER_COUNT; i++) {
      const workerDbName = `boostedbudget_test_worker_${i}`;
      await runMigrations(BASE_DB_URL, workerDbName);
    }

    console.log('\n✅ E2E test environment setup complete!\n');
    console.log('Worker database URLs:');
    for (let i = 0; i < WORKER_COUNT; i++) {
      console.log(`  Worker ${i}: TEST_DATABASE_URL_${i}=${process.env[`TEST_DATABASE_URL_${i}`]}`);
    }
    console.log('');

  } catch (error) {
    console.error('\n❌ Setup failed:', error);
    throw error;
  } finally {
    await adminPool.end();
  }
}

export default globalSetup;
