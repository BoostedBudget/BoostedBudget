/**
 * Global Setup Script for E2E Scenarios
 *
 * Creates isolated test databases for each Playwright worker to prevent test pollution.
 * Each worker gets its own database (boostedbudget_test_worker_0, _1, _2, _3).
 *
 * Issue #331: E2E Test Scenarios Framework
 */

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
  // Support both postgres:// and postgresql:// protocols
  // Database name is optional (for connecting to admin 'postgres' database)
  const match = url.match(/^(postgres(?:ql)?:\/\/)([^:]+):([^@]+)@([^:]+):(\d+)(?:\/([^?]+))?(\?.*)?$/);
  if (!match) {
    const errorMsg = `Invalid database URL format: ${url}\nExpected: postgres://user:pass@host:port or postgres://user:pass@host:port/dbname`;
    console.error(`\n❌ ERROR: ${errorMsg}\n`);
    throw new Error(errorMsg);
  }

  return {
    protocol: match[1],
    user: match[2],
    password: match[3],
    host: match[4],
    port: match[5],
    database: match[6] || 'postgres', // Default to 'postgres' if not specified
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
      console.log(`  📦 Creating database: ${dbName}`);
      await adminPool.query(`CREATE DATABASE ${dbName}`);
      console.log(`  ✅ Database ${dbName} created`);
    } else {
      console.log(`  ℹ️  Database ${dbName} already exists (skipping)`);
    }
  } catch (error) {
    console.error(`\n❌ Failed to create database ${dbName}`);
    console.error('Error details:', error);
    throw error; // Re-throw to fail the entire setup
  }
}

/**
 * Main setup function
 */
async function globalSetup(): Promise<void> {
  console.log('\n🚀 Setting up E2E test environment...\n');

  // Validate environment first
  if (!BASE_DB_URL || BASE_DB_URL === 'postgres://testuser:testpass@localhost:5433') {
    console.error('⚠️  WARNING: Using default TEST_DATABASE_URL');
    console.error('   Set TEST_DATABASE_URL environment variable for proper database connection\n');
  }

  console.log('📋 Configuration:');
  console.log(`  Base URL: ${BASE_DB_URL}`);
  console.log(`  Worker count: ${WORKER_COUNT}\n`);

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

    console.log('\n✅ All worker databases created successfully!\n');
    console.log('Created databases:');
    console.log(`  ${MAIN_DB_NAME} (main test database)`);
    for (let i = 0; i < WORKER_COUNT; i++) {
      console.log(`  boostedbudget_test_worker_${i} (worker ${i})`);
    }
    console.log('\n💡 Note: Migrations will be run separately by the workflow\n');

  } catch (error) {
    console.error('\n❌ Setup failed:', error);
    throw error;
  } finally {
    await adminPool.end();
  }
}

// Execute setup when run directly
globalSetup()
  .then(() => {
    console.log('✅ Setup completed successfully\n');
    process.exit(0);
  })
  .catch((error) => {
    console.error('\n❌ Setup failed:', error);
    if (error instanceof Error) {
      console.error('Stack trace:', error.stack);
    }
    process.exit(1);
  });
